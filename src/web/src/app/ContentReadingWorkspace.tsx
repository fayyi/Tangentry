import { useSubtitleAssistance } from './useSubtitleAssistance'
import { useFrameCapture } from './useFrameCapture'
import { ReaderCaptureAction } from '../features/player/CaptureButton'
import { captureVideoFrame } from '../shared/media/captureFrame'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { ContentItem, Discussion, SubtitleTrack, DownloadJob, ReadingNote, TextAnchor, PDFAnchor, SubtitleReference, SavedSubtitle } from '../shared/api/types'
import { MediaReader } from '../features/reader/MediaReader'
import { PodcastTranscript } from '../features/subtitles/PodcastTranscript'
import { NotesPanel } from '../features/notes/NotesPanel'
import type { NoteTarget } from '../features/notes/NotesPanel'
import { usePlaybackPosition } from '../features/player/usePlaybackPosition'
import type { PlaybackHandle } from '../shared/media/types'
import { AIInsights } from '../features/ai/AIInsights'
import { useSummary } from '../features/ai/useSummary'
import { SummaryControl } from '../features/ai/SummaryControl'
import { ReaderActions } from '../shared/reader/ReaderActions'
import { TranscriptionControls } from '../features/subtitles/TranscriptionControls'
import { request } from '../shared/api/client'
import { discussionSummary } from '../features/reader/DiscussionReader'

export function ContentReadingWorkspace({ id, linkedNoteId, initialItem, jobs, jobsLoading, onReturn, onItem, onCreator }: { id: string; linkedNoteId?: string; initialItem?: ContentItem; jobs: DownloadJob[]; jobsLoading?: boolean; onReturn: () => void; onItem?: (item:ContentItem) => void; onCreator?: (platform:string, authorID:string) => void }) {
  const [item,setItem]=useState<ContentItem|null>(initialItem??null)
  const [position,setPosition]=usePlaybackPosition()
  const [savedNotes, setSavedNotes] = useState<ReadingNote[]>([]), [unresolved, setUnresolved] = useState<string[]>([])
  const [textFocus, setTextFocus] = useState<{id:string;token:number;allowReturn?:boolean}|null>(null), [activeNoteIds, setActiveNoteIds] = useState<string[]>([])
  const media = useRef<HTMLMediaElement | null>(null), playback = useRef<PlaybackHandle | null>(null), sequence = useRef(0)
  const [ready, setReady] = useState(false), [target, setTarget] = useState<NoteTarget | null>(null)
  const [showTranscription, setShowTranscription] = useState(false)
  const [discussionRepliesLoaded, setDiscussionRepliesLoaded] = useState(false)
  const resolvedItem = useCallback((value:ContentItem) => { setItem(value);onItem?.(value) },[onItem])
  const discussionChanged = useCallback((discussion: Discussion) => {
    setDiscussionRepliesLoaded(discussion.loadedPosts > 1)
  }, [])
  useEffect(() => setDiscussionRepliesLoaded(false), [id])
  const getPosition = useCallback(() => { const node = media.current; if (node && node.readyState >= 1 && !node.error && !(node.tagName === 'AUDIO' && node.seeking) && (!(node instanceof HTMLVideoElement) || node.videoWidth > 0) && Number.isFinite(node.currentTime)) return node.currentTime; return playback.current?.getPosition() ?? null }, [])
  const quote = useCallback((text: string, textAnchor?: TextAnchor) => setTarget({ contentId: id, quote: text, textAnchor, token: ++sequence.current }), [id])
  const pdfQuote = useCallback((text: string, pdfAnchor: PDFAnchor) => setTarget({contentId:id,quote:text,pdfAnchor,token:++sequence.current}),[id])
  const jumpText = useCallback((noteId: string, fromLink = false) => { setTextFocus({id:noteId,token:++sequence.current,allowReturn:!fromLink}) }, [])
  const openNotes = useCallback((ids: string[]) => setActiveNoteIds(ids), [])
  const seek = useCallback((seconds: number) => {
    const node = media.current
    if (node) {
      if (!Number.isFinite(seconds) || seconds < 0 || !Number.isFinite(node.duration) || seconds > node.duration) return false
      node.currentTime = seconds
      return true
    }
    return playback.current?.seek(seconds) ?? false
  }, [])
  const preparePlayback = useCallback(() => {
    const node = media.current
    if (!node || node.error) return false
    node.preload = 'metadata'
    node.load()
    return true
  }, [])
  const noteAt=useCallback((seconds:number,subtitleReference?:SubtitleReference)=>setTarget({contentId:id,positionSeconds:seconds,subtitleReference,token:++sequence.current}),[id])
  const [captions,setCaptions]=useState<SubtitleTrack|null>(null)
  const capture = useCallback(() => media.current instanceof HTMLVideoElement ? captureVideoFrame(media.current) : playback.current?.captureFrame?.(), [])
  const captureFrame = useFrameCapture(capture, captions)
  const captureAction = useRef<(() => void) | null>(null)
  const requestScreenshot = useCallback(() => captureAction.current?.(), [])
  const assistance=useSubtitleAssistance(id,captions,ready?seek:undefined,{contentId:id,title:item?.title,url:item?.url}, item?.kind !== 'pdf')
  const discussionText = item?.discussion?.posts?.length ? discussionSummary(item.discussion, discussionRepliesLoaded) : ''
  const summaryText = discussionText || item?.description?.trim() || item?.articleBlocks?.map(block => `${block.text ?? ''}${block.alt ? `\n${block.alt}` : ''}`).join('\n').trim() || item?.images?.map(image => image.alt).filter(Boolean).join('\n').trim() || ''
  const summaryInputText = summaryText || (item?.kind === 'image' ? item.title : '')
  const textSummary = useSummary(id, 'content', captions ?? undefined, ready ? seek : undefined, captions?.cues.length ? undefined : summaryInputText)
  const activeSummary = assistance?.summary ?? textSummary
  const contextText = [summaryText, captions?.cues.map(cue => cue.text).join('\n')].filter(Boolean).join('\n')
  const locateQuote = useCallback((quote: string) => {
    const root = window.document.querySelector<HTMLElement>('.article-text')
    if (!root || !quote.trim()) return
    const walker = window.document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
    let node: Node | null
    while ((node = walker.nextNode())) {
      const start = node.textContent?.indexOf(quote) ?? -1
      if (start < 0) continue
      const range = window.document.createRange(); range.setStart(node, start); range.setEnd(node, start + quote.length)
      const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range)
      ;(node.parentElement as HTMLElement | null)?.scrollIntoView({ block: 'center', behavior: 'smooth' })
      return
    }
  }, [])
  const needsTranscription = !captions?.cues.length && (item?.kind === 'audio' || item?.kind === 'video')
  const insights = <AIInsights summary={activeSummary} onSeek={ready ? seek : undefined} onLocateQuote={locateQuote} contentId={id} context={contextText} revision={activeSummary.inspection?.settings.revision ?? 0}/>
  const summaryEmptyMessage = needsTranscription ? '当前没有字幕或可用文字。' : !summaryText ? '当前没有可用于摘要的正文、图片替代文本或 OCR 文字。' : undefined
  const summaryEmptyAction = needsTranscription ? (!showTranscription ? <button type="button" className="button button-secondary" onClick={() => setShowTranscription(true)}>先转录再摘要</button> : <TranscriptionControls contentId={id} video={item?.kind === 'video'} onComplete={() => { setShowTranscription(false); void request<{ tracks: SavedSubtitle[] }>(`/saved-subtitles?contentId=${encodeURIComponent(id)}`).then(value => setCaptions(value.tracks[0]?.track ?? null)).catch(() => {}) }}/>) : null
  return <>{item?.kind === 'video' && <ReaderCaptureAction onCapture={requestScreenshot}/>}<ReaderActions>{!assistance && (item?.kind !== 'article' || item.platform === 'linuxdo') && item?.kind !== 'pdf' && <SummaryControl summary={activeSummary} source={{contentId:id,title:item?.title,url:item?.url}} emptyMessage={summaryEmptyMessage} emptyAction={summaryEmptyAction}/>}</ReaderActions><MediaReader id={id} initialItem={initialItem} jobs={jobs} jobsLoading={jobsLoading} onReturn={onReturn} onCreator={onCreator} mediaRef={media} playbackRef={playback} onReady={setReady} onPosition={setPosition} onItem={resolvedItem} onDiscussionChange={discussionChanged} onQuote={quote} onPDFQuote={pdfQuote} annotations={savedNotes} textFocus={textFocus} onOpenNotes={openNotes} onUnresolved={setUnresolved} insights={insights} reading={item&&(item.kind==='audio'&&(item.platform==='rss'||item.platform==='xiaoyuzhou')||item.kind==='video'&&(item.platform==='douyin'||item.platform==='xiaohongshu'||item.platform==='rss'&&Boolean(item.mediaUrl)))?<PodcastTranscript assistance={assistance} onTrackChange={setCaptions} item={item} position={position} onSeek={ready?seek:undefined} onNoteAt={noteAt}/>:undefined} notes={<NotesPanel contentId={id} linkedNoteId={linkedNoteId} target={target} subtitleTrack={captions} captureActionRef={captureAction} onNotesChange={setSavedNotes} onJumpText={item?.kind==='article'||item?.kind==='image'||item?.platform==='linuxdo'?jumpText:undefined} onJumpPDF={item?.kind==='pdf'?jumpText:undefined} unresolvedAnchorIds={unresolved} activeNoteIds={activeNoteIds} getPosition={getPosition} captureFrame={item?.kind === 'video' ? captureFrame : undefined} playbackPosition={position} playbackHint={item?.kind === "audio" || item?.kind === "video" ? "开始写笔记时自动关联当前播放时间。" : undefined} onSeek={ready?seek:undefined} onPreparePlayback={item?.kind==='audio'||item?.kind==='video'?preparePlayback:undefined}/>} /></>
}
