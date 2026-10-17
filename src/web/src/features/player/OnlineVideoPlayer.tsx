import { Play } from 'lucide-react'
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { BilibiliStreamPlayer } from './BilibiliStreamPlayer'
import { YouTubeEmbedPlayer } from '../../shared/media/YouTubeEmbedPlayer'
import type { PlaybackHandle } from '../../shared/media/types'
import type { ReactNode } from 'react'
import type { SubtitleTrack } from '../../shared/api/types'

export const OnlineVideoPlayer = forwardRef<PlaybackHandle, {
  platform: string; id: string; title: string; cover?: string; captions?: SubtitleTrack | null
  onPosition?: (seconds: number | null) => void; onReady?: (ready: boolean) => void
  onProgress?: (position: number, duration: number, immediate?: boolean) => void; onPlaying?: (playing: boolean) => void; overlay?: ReactNode
}>(function OnlineVideoPlayer({ platform, id, title, cover = '', captions, onPosition, onReady, onProgress, onPlaying, overlay }, ref) {
  const [started, setStarted] = useState(false)
  const [start, setStart] = useState(0)
  const controlled = useRef<PlaybackHandle>(null)
  const valid = platform === 'youtube' ? /^[A-Za-z0-9_-]{11}$/.test(id) : /^BV[0-9A-Za-z]{10}$/.test(id)
  useEffect(() => {
    if (started) return
    onReady?.(valid); onPosition?.(null)
    return () => { onReady?.(false); onPosition?.(null) }
  }, [started, valid, onReady, onPosition])
  useImperativeHandle(ref, () => ({ captureFrame: () => controlled.current?.captureFrame?.() ?? Promise.reject(new Error(started ? (platform === 'youtube' ? 'YouTube 嵌入播放器不允许读取画面，下载到本机后即可截屏。' : '播放器还没有准备好，请稍后再截屏。') : '请先开始播放，再截屏。')), getPosition: () => controlled.current?.getPosition() ?? null, seek: seconds => {
    if (controlled.current) return controlled.current.seek(seconds)
    if (!valid || !Number.isFinite(seconds) || seconds < 0 || seconds > 604800) return false
    setStart(seconds); setStarted(true)
    return true
  } }), [valid, started, platform])
  return <section className="online-player" aria-label="在线播放">
    {started ? platform === 'youtube' ? <YouTubeEmbedPlayer ref={controlled} id={id} title={title} start={start} onPosition={onPosition} onReady={onReady} onProgress={onProgress} onPlaying={onPlaying}/> : <BilibiliStreamPlayer ref={controlled} id={id} title={title} start={start} captions={captions} onPosition={onPosition} onReady={onReady} onProgress={onProgress} onPlaying={onPlaying} overlay={overlay}/> : <button className="online-play" aria-label={'播放视频 ' + title} onClick={() => setStarted(true)}>
      {cover && <img src={cover} alt="" referrerPolicy="no-referrer" />}
      <span><Play size={24} fill="currentColor" />播放视频</span>
    </button>}
  </section>
})
