import { useSubtitleAssistance } from './useSubtitleAssistance'
import { useFrameCapture } from './useFrameCapture'
import { PlayerCaptureOverlay, ReaderCaptureAction } from '../features/player/CaptureButton'
import { useCallback, useMemo, useRef, useState } from 'react'
import type { ContentItem, SubtitleTrack, SubtitleReference } from '../shared/api/types'
import { SubtitlePage } from '../features/subtitles/SubtitlePage'
import { NotesPanel } from '../features/notes/NotesPanel'
import type { NoteTarget } from '../features/notes/NotesPanel'
import { OnlineVideoPlayer } from '../features/player/OnlineVideoPlayer'
import { LocalVideoPlayer } from '../features/player/LocalVideoPlayer'
import type { LocalPlayerHandle, LocalVideoSource } from '../features/player/LocalVideoPlayer'
import { useReadingProgress } from '../features/progress/useReadingProgress'
import { ReadingProgressPanel } from '../features/progress/ReadingProgressPanel'
import { usePlaybackPosition } from '../features/player/usePlaybackPosition'
import { useSyncReaderPlayback } from '../shared/reader/ReaderActions'
import { AIInsights } from '../features/ai/AIInsights'
import { useSummary } from '../features/ai/useSummary'
import { SummaryControl } from '../features/ai/SummaryControl'
import { ReaderActions } from '../shared/reader/ReaderActions'

// Composition belongs to app: playback, subtitles and notes communicate through
// callbacks/contracts without importing each other's feature implementations.
export function ReadingWorkspace({ query, linkedNoteId, initialItem, onReturn, sources, loading, error, onDownloads, onRefresh }: {
  query: string; linkedNoteId?: string; initialItem?: ContentItem; onReturn: () => void; sources: LocalVideoSource[]
  loading: boolean; error: string; onDownloads: () => void; onRefresh: () => void
}) {
  const player = useRef<LocalPlayerHandle>(null)
  const sequence = useRef(0)
  const [target, setTarget] = useState<NoteTarget | null>(null)
  const [position, setPosition] = usePlaybackPosition()
  const [ready, setReady] = useState(false)
  const [playing, setPlaying] = useState(false)
  useSyncReaderPlayback(playing)
  const progress = useReadingProgress(`bilibili:${query}`)
  const [captions, setCaptions] = useState<SubtitleTrack | null>(null)
  const seek = useCallback((seconds: number) => player.current?.seek(seconds) ?? false, [])
  const assistance = useSubtitleAssistance(`bilibili:${query}`, captions, ready ? seek : undefined, { contentId: `bilibili:${query}`, title: initialItem?.title, url: initialItem?.url ?? `https://www.bilibili.com/video/${query}/` })
  const noSubtitleSummary = useSummary(`bilibili:${query}`, 'subtitle', undefined, ready ? seek : undefined)
  const getPosition = useCallback(() => player.current?.getPosition() ?? null, [])
  const getDownloadID = useCallback(() => player.current?.getSourceID?.() ?? null, [])
  const capture = useCallback(() => player.current?.captureFrame?.(), [])
  const captureFrame = useFrameCapture(capture, captions)
  const captureAction = useRef<(() => void) | null>(null)
  const requestScreenshot = useCallback(() => captureAction.current?.(), [])
  const noteAt = useCallback((positionSeconds: number, subtitleReference?: SubtitleReference) => setTarget({ contentId: `bilibili:${query}`, positionSeconds, subtitleReference, token: ++sequence.current }), [query])
  const notes = useMemo(() => <NotesPanel contentId={`bilibili:${query}`} linkedNoteId={linkedNoteId} target={target} subtitleTrack={captions} captureActionRef={captureAction} getPosition={getPosition} getDownloadID={sources.length ? getDownloadID : undefined} captureFrame={captureFrame} playbackPosition={position} playbackHint="开始写笔记时自动关联当前播放时间。" onSeek={ready ? seek : undefined} />, [query, linkedNoteId, target, captions, ready, seek, getPosition, getDownloadID, captureFrame, sources.length, position])
  const toolbarSummary = !assistance ? <ReaderActions><SummaryControl summary={noSubtitleSummary} source={{contentId: `bilibili:${query}`, title: initialItem?.title, url: initialItem?.url ?? `https://www.bilibili.com/video/${query}/`}}/></ReaderActions> : null
  return <>{toolbarSummary}<SubtitlePage query={query} initialItem={initialItem} onReturn={onReturn} notes={notes} insights={assistance ? <AIInsights summary={assistance.summary} onSeek={ready ? seek : undefined} contentId={`bilibili:${query}`} context={initialItem?.description ?? ''} revision={assistance.summary.inspection?.settings.revision ?? 0}/> : undefined} position={position} onSeek={ready ? seek : undefined} onTrackChange={setCaptions} assistance={assistance} onNoteAt={noteAt} player={<><ReaderCaptureAction onCapture={requestScreenshot}/>{!loading&&sources.length===0?<OnlineVideoPlayer captions={captions} ref={player} onPosition={setPosition} onReady={setReady} onProgress={progress.observe} onPlaying={setPlaying} platform="bilibili" id={query} title={initialItem?.title??query} cover={initialItem?.thumbnailUrl} overlay={<PlayerCaptureOverlay onCapture={requestScreenshot}/>}/>:<LocalVideoPlayer ref={player} sources={sources} loading={loading} error={error} captions={captions} onPosition={setPosition} onReady={setReady} onDownloads={onDownloads} onRefresh={onRefresh} onProgress={progress.observe} onPlaying={setPlaying} overlay={<PlayerCaptureOverlay onCapture={requestScreenshot}/>} />}{(sources.length>0 || progress.saved || progress.readError || progress.writeError || position !== null) && <ReadingProgressPanel progress={progress} ready={ready} playing={playing} position={position} onSeek={seek} />}</>} /></>
}
