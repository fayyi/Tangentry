import { useSubtitleAssistance } from './useSubtitleAssistance'
import { useFrameCapture } from './useFrameCapture'
import { ReaderCaptureAction } from '../features/player/CaptureButton'
import { captureVideoFrame } from '../shared/media/captureFrame'
import { useCallback, useRef, useState } from 'react'
import type { ContentItem, SubtitleTrack, DownloadJob, ReadingNote, TextAnchor, PDFAnchor, SubtitleReference } from '../shared/api/types'
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

export function ContentReadingWorkspace({ id, linkedNoteId, initialItem, jobs, jobsLoading, onReturn, onItem }: { id: string; linkedNoteId?: string; initialItem?: ContentItem; jobs: DownloadJob[]; jobsLoading?: boolean; onReturn: () => void; onItem?: (item:ContentItem) => void }) {
  const [item,setItem]=useState<ContentItem|null>(initialItem??null)
  const [position,setPosition]=usePlaybackPosition()
  const [savedNotes, setSavedNotes] = useState<ReadingNote[]>([]), [unresolved, setUnresolved] = useState<string[]>([])
  const [textFocus, setTextFocus] = useState<{id:string;token:number;allowReturn?:boolean}|null>(null), [activeNoteIds, setActiveNoteIds] = useState<string[]>([])
  const media = useRef<HTMLMediaElement | null>(null), playback = useRef<PlaybackHandle | null>(null), sequence = useRef(0)
  const [ready, setReady] = useState(false), [target, setTarget] = useState<NoteTarget | null>(null)
  const resolvedItem = useCallback((value:ContentItem) => { setItem(value);onItem?.(value) },[onItem])
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
  const assistance=useSubtitleAssistance(id,captions,ready?seek:undefined,{contentId:id,title:item?.title,url:item?.url})
  const textSummary = useSummary(id, 'content', undefined, ready ? seek : undefined, item?.description)
  const activeSummary = assistance?.summary ?? textSummary
  const insights = <AIInsights summary={activeSummary} onSeek={ready ? seek : undefined} contentId={id} context={item?.description ?? ''} revision={activeSummary.inspection?.settings.revision ?? 0}/>
  return <>{item?.kind === 'video' && <ReaderCaptureAction onCapture={requestScreenshot}/>}<ReaderActions>{!assistance && item?.description && <SummaryControl summary={activeSummary} source={{contentId:id,title:item.title,url:item.url}}/>}</ReaderActions><MediaReader id={id} initialItem={initialItem} jobs={jobs} jobsLoading={jobsLoading} onReturn={onReturn} mediaRef={media} playbackRef={playback} onReady={setReady} onPosition={setPosition} onItem={resolvedItem} onQuote={quote} onPDFQuote={pdfQuote} annotations={savedNotes} textFocus={textFocus} onOpenNotes={openNotes} onUnresolved={setUnresolved} insights={insights} reading={item&&(item.kind==='audio'&&(item.platform==='rss'||item.platform==='xiaoyuzhou')||item.kind==='video'&&(item.platform==='douyin'||item.platform==='xiaohongshu'||item.platform==='rss'&&Boolean(item.mediaUrl)))?<PodcastTranscript assistance={assistance} onTrackChange={setCaptions} item={item} position={position} onSeek={ready?seek:undefined} onNoteAt={noteAt}/>:undefined} notes={<NotesPanel contentId={id} linkedNoteId={linkedNoteId} target={target} subtitleTrack={captions} captureActionRef={captureAction} onNotesChange={setSavedNotes} onJumpText={item?.kind==='article'||item?.kind==='image'?jumpText:undefined} onJumpPDF={item?.kind==='pdf'?jumpText:undefined} unresolvedAnchorIds={unresolved} activeNoteIds={activeNoteIds} getPosition={getPosition} captureFrame={item?.kind === 'video' ? captureFrame : undefined} playbackPosition={position} playbackHint={item?.kind === "audio" || item?.kind === "video" ? "开始写笔记时自动关联当前播放时间。" : undefined} onSeek={ready?seek:undefined} onPreparePlayback={item?.kind==='audio'||item?.kind==='video'?preparePlayback:undefined}/>} /></>
}
