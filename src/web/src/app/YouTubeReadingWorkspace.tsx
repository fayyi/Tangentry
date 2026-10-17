import { useSubtitleAssistance } from './useSubtitleAssistance'
import { useFrameCapture } from './useFrameCapture'
import { PlayerCaptureOverlay, ReaderCaptureAction } from '../features/player/CaptureButton'
import { useCallback, useMemo, useRef, useState } from 'react'
import type { ContentItem, DownloadJob, SubtitleTrack, SubtitleReference } from '../shared/api/types'
import { MediaReader } from '../features/reader/MediaReader'
import { OnlineVideoPlayer } from '../features/player/OnlineVideoPlayer'
import { LocalVideoPlayer } from '../features/player/LocalVideoPlayer'
import type { LocalPlayerHandle } from '../features/player/LocalVideoPlayer'
import { SubtitleReader } from '../features/subtitles/SubtitleReader'
import { NotesPanel } from '../features/notes/NotesPanel'
import type { NoteTarget } from '../features/notes/NotesPanel'
import { useReadingProgress } from '../features/progress/useReadingProgress'
import { ReadingProgressPanel } from '../features/progress/ReadingProgressPanel'
import { usePlaybackPosition } from '../features/player/usePlaybackPosition'
import { useSyncReaderPlayback } from '../shared/reader/ReaderActions'

// App composes media and captions. Feature internals stay independent.
export function YouTubeReadingWorkspace({ id, linkedNoteId, initialItem, jobs, loading, error, onReturn, onDownloads, onRefresh }: {
  id: string; linkedNoteId?: string; initialItem?: ContentItem; jobs: DownloadJob[]; loading: boolean; error: string
  onReturn: () => void; onDownloads: () => void; onRefresh: () => void
}) {
  const player = useRef<LocalPlayerHandle>(null)
  const sequence = useRef(0)
  const [target, setTarget] = useState<NoteTarget | null>(null)
  const [ready, setReady] = useState(false)
  const [position, setPosition] = usePlaybackPosition()
  const [playing, setPlaying] = useState(false)
  useSyncReaderPlayback(playing)
  const progress = useReadingProgress(id)
  const [captions, setCaptions] = useState<SubtitleTrack | null>(null)
  const [exportItem, setExportItem] = useState(initialItem)
  const videoId = id.slice('youtube:'.length)
  const valid = /^[A-Za-z0-9_-]{11}$/.test(videoId)
  const url = 'https://www.youtube.com/watch?v=' + videoId
  const sources = useMemo(() => jobs.filter(job => job.state === 'completed' && job.mode !== 'audio' && job.url === url).map(job => ({
    id: job.id, label: `${job.maxHeight ? `最高 ${job.maxHeight}p` : '原画'} · ${new Date(job.createdAt * 1000).toLocaleString('zh-CN')}`, url: `/api/downloads/${encodeURIComponent(job.id)}/media`,
  })), [jobs, url])
  const seek = useCallback((seconds: number) => player.current?.seek(seconds) ?? false, [])
  const assistance = useSubtitleAssistance(id, captions, ready ? seek : undefined, { contentId: id, title: initialItem?.title, url })
  const getPosition = useCallback(() => player.current?.getPosition() ?? null, [])
  const getDownloadID = useCallback(() => player.current?.getSourceID?.() ?? null, [])
  const capture = useCallback(() => player.current?.captureFrame?.(), [])
  const captureFrame = useFrameCapture(capture, captions)
  const captureAction = useRef<(() => void) | null>(null)
  const requestScreenshot = useCallback(() => captureAction.current?.(), [])
  const noteAt = useCallback((positionSeconds: number, subtitleReference?: SubtitleReference) => setTarget({ contentId: id, positionSeconds, subtitleReference, token: ++sequence.current }), [id])
  if (!valid) return <p className="subtitle-error" role="alert">阅读地址缺少正确的 YouTube 视频标识，请返回列表选择视频。</p>
  return <MediaReader id={id} initialItem={initialItem} jobs={jobs} onReturn={onReturn} onItem={setExportItem} onSeek={ready ? seek : undefined}
    video={<><ReaderCaptureAction onCapture={requestScreenshot}/>{!loading&&sources.length===0?<OnlineVideoPlayer ref={player} onPosition={setPosition} onReady={setReady} onProgress={progress.observe} onPlaying={setPlaying} platform="youtube" id={videoId} title={initialItem?.title??videoId} cover={initialItem?.thumbnailUrl} overlay={<PlayerCaptureOverlay onCapture={requestScreenshot}/>}/>:<LocalVideoPlayer ref={player} sources={sources} loading={loading} error={error} captions={captions} onReady={setReady} onPosition={setPosition} onDownloads={onDownloads} onRefresh={onRefresh} onProgress={progress.observe} onPlaying={setPlaying} overlay={<PlayerCaptureOverlay onCapture={requestScreenshot}/>} />}{(progress.saved || progress.readError || progress.writeError || position !== null) && <ReadingProgressPanel progress={progress} ready={ready} playing={playing} position={position} onSeek={seek}/>}</>}
    reading={<SubtitleReader source="youtube" item={{ platformId: videoId, url, title: exportItem?.title, author: exportItem?.author }} position={position} onSeek={ready ? seek : undefined} onTrackChange={setCaptions} assistance={assistance} onNoteAt={noteAt} />}
    notes={<NotesPanel contentId={id} linkedNoteId={linkedNoteId} target={target} subtitleTrack={captions} captureActionRef={captureAction} getPosition={getPosition} getDownloadID={sources.length ? getDownloadID : undefined} captureFrame={captureFrame} playbackPosition={position} playbackHint={"开始写笔记时自动关联当前播放时间。"} onSeek={ready ? seek : undefined} />} />
}
