import { Button } from '../../components/ui'
import { LoaderCircle, MonitorPlay, RefreshCw } from 'lucide-react'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { PlaybackHandle } from '../../shared/media/types'
import type { ReactNode } from 'react'
import type { SubtitleTrack } from '../../shared/api/types'
import { toWebVTT } from './captions'
import { BilibiliEmbedPlayer } from '../../shared/media/BilibiliEmbedPlayer'
import { useNativePlaybackClock } from './useNativePlaybackClock'
import { captureVideoFrame } from '../../shared/media/captureFrame'

// The application proxy supplies a combined MP4. Notes and subtitles read the
// same native clock; no iframe guessing, wall-clock estimates or media download.
export const BilibiliStreamPlayer = forwardRef<PlaybackHandle, {
  id: string; title: string; start?: number; captions?: SubtitleTrack | null; onPosition?: (seconds: number | null) => void; onReady?: (ready: boolean) => void
  onProgress?: (position: number, duration: number, immediate?: boolean) => void; onPlaying?: (playing: boolean) => void; overlay?: ReactNode
}>(function BilibiliStreamPlayer({ id, title, start = 0, captions, onPosition, onReady, onProgress, onPlaying, overlay }, ref) {
  const video = useRef<HTMLVideoElement>(null)
  const lastVideo = useRef<HTMLVideoElement | null>(null)
  const nodeRef = useCallback((node: HTMLVideoElement | null) => { video.current = node; if (node) lastVideo.current = node }, [])
  const [session, setSession] = useState<{ id: string; quality: number } | null>(null)
  useNativePlaybackClock(video, session?.id, onPosition)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [embedded, setEmbedded] = useState(false)
  const embed = useRef<PlaybackHandle>(null)
  const resume = useRef(start)
  const connectionTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const bufferTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const decoded = useRef(false)
  const engaged = useRef(false)
  const clearConnectionTimer = () => {
    if (connectionTimer.current !== null) clearTimeout(connectionTimer.current)
    connectionTimer.current = null
  }
  const clearBufferTimer = () => {
    if (bufferTimer.current !== null) clearTimeout(bufferTimer.current)
    bufferTimer.current = null
  }
  const watchBuffer = () => {
    const node = video.current
    if (!decoded.current || !node || node.paused || node.ended || node.playbackRate === 0 || node.readyState >= 3 || bufferTimer.current !== null) return
    const stalledAt = node.currentTime
    bufferTimer.current = setTimeout(() => {
      bufferTimer.current = null
      if (video.current !== node || node.paused || node.ended || node.playbackRate === 0 || node.readyState >= 3) return
      if (node.currentTime !== stalledAt) { watchBuffer(); return }
      if (Number.isFinite(node.currentTime)) resume.current = node.currentTime
      onReady?.(false); onPosition?.(null)
      setError('同步播放缓冲超时'); setEmbedded(true)
    }, 12000)
  }
  const [captionURL,setCaptionURL] = useState('')
  useEffect(()=>{ if(!captions){setCaptionURL('');return};const url=URL.createObjectURL(new Blob([toWebVTT(captions.cues)],{type:'text/vtt;charset=utf-8'}));setCaptionURL(url);return()=>URL.revokeObjectURL(url) },[captions])
  const getPosition = () => {
    const node = video.current
    return node && node.readyState >= 1 && !node.error && !error && Number.isFinite(node.currentTime) ? node.currentTime : null
  }
  useImperativeHandle(ref, () => ({ captureFrame: () => embedded ? embed.current?.captureFrame?.() ?? Promise.reject(new Error('官方播放器还没有准备好，请稍后再截屏。')) : captureVideoFrame(error ? null : video.current), getPosition: () => embedded ? embed.current?.getPosition() ?? null : getPosition(), seek: seconds => {
    if (embedded) return embed.current?.seek(seconds) ?? false
    const node = video.current
    if (!node || getPosition() === null || !Number.isFinite(seconds) || seconds < 0 || seconds > node.duration) return false
    try { node.currentTime = seconds; onPosition?.(node.currentTime); return true } catch { return false }
  } }), [embedded, error, onPosition])

  useEffect(() => {
    if (embedded) return
    const controller = new AbortController()
    let active = true, token: string | undefined
    decoded.current = false
    engaged.current = false
    setSession(null); setError(''); onReady?.(false); onPosition?.(null)
    connectionTimer.current = setTimeout(() => {
      if (!active) return
      controller.abort()
      onReady?.(false); onPosition?.(null)
      setError('同步播放连接超时'); setEmbedded(true)
    }, 20000)
    void request<{ id: string; quality: number }>('/playback/bilibili/' + encodeURIComponent(id), { method: 'POST', signal: controller.signal }).then(value => {
      token = value.id
      if (active) setSession(value)
      else void request('/playback/sessions/' + token, { method: 'DELETE', keepalive: true }).catch(() => {})
    }).catch(cause => {
      if (active) { setError(cause instanceof Error ? cause.message : '视频暂时无法连接。'); setEmbedded(true) }
    })
    return () => {
      active = false; controller.abort()
      clearConnectionTimer()
      clearBufferTimer()
      const node = lastVideo.current
      if (node && engaged.current && node.readyState >= 1 && !node.error) onProgress?.(node.currentTime, node.duration, true)
      node?.pause(); if (node) { node.removeAttribute('src'); node.load() }
      onPlaying?.(false)
      onReady?.(false); onPosition?.(null)
      if (token) void request('/playback/sessions/' + token, { method: 'DELETE', keepalive: true }).catch(() => {})
    }
  }, [id, attempt, embedded, onReady, onPosition, onProgress, onPlaying])

  const rememberTarget = useCallback((seconds: number) => { resume.current = seconds }, [])
  const report = () => {
    const seconds = getPosition()
    if (seconds !== null) resume.current = seconds
    onPosition?.(seconds)
  }
  const checkpoint = (immediate = false) => {
    const node = video.current
    if (node && engaged.current && node.readyState >= 1 && !node.error) onProgress?.(node.currentTime, node.duration, immediate)
  }
  if (embedded) return <div className="bilibili-stream-player">
    <BilibiliEmbedPlayer ref={embed} id={id} title={title} start={resume.current} onReady={onReady} onPosition={onPosition} onProgress={onProgress} onPlaying={onPlaying} onSeekTarget={rememberTarget}/>
    <div className="player-footer"><span className="field-hint">{error ? '已切换到 B站官方播放器' : 'B站官方播放器'}</span><Button variant="secondary" className="button button-secondary" onClick={() => { setEmbedded(false); setAttempt(value => value + 1) }}><RefreshCw size={14}/>重试同步播放</Button><a className="button button-secondary" href={`https://www.bilibili.com/video/${id}/?t=${Math.floor(resume.current)}`} target="_blank" rel="noopener noreferrer">打开原视频</a></div>
  </div>
  return <div className="bilibili-stream-player">
    <div className="online-video-stage player-capture-stage">
      {session && overlay}
      {session ? <video ref={nodeRef} className="local-video" aria-label="B站在线视频播放器" title={title} controls playsInline autoPlay preload="auto" src={'/api/playback/sessions/' + session.id + '/media'}
        onLoadedMetadata={() => { const node = video.current; if (node && resume.current > 0 && resume.current < node.duration) node.currentTime = resume.current; onReady?.(true); report() }}
        onLoadedData={() => { decoded.current = true; clearConnectionTimer(); clearBufferTimer() }}
        onWaiting={watchBuffer} onStalled={watchBuffer}
        onPlaying={clearBufferTimer} onCanPlay={clearBufferTimer}
        onTimeUpdate={() => { report(); checkpoint() }} onSeeking={() => { engaged.current = true; clearBufferTimer(); report() }} onSeeked={() => { report(); checkpoint(true) }}
        onPlay={() => { engaged.current = true; onPlaying?.(true); checkpoint(true) }}
        onPause={() => { clearBufferTimer(); onPlaying?.(false); report(); checkpoint(true) }} onEnded={() => { clearBufferTimer(); onPlaying?.(false); report(); checkpoint(true) }}
        onError={() => { const node = video.current; if (node && node.readyState >= 1 && Number.isFinite(node.currentTime)) resume.current = node.currentTime; onPlaying?.(false); onReady?.(false); onPosition?.(null); setError('同步播放暂不可用'); setEmbedded(true) }}>{captionURL&&captions&&<track key={captionURL} kind="captions" src={captionURL} srcLang={captions.language==='ai-zh'?'zh':captions.language} label={captions.label} default/>}</video>
        : !error && <p role="status"><LoaderCircle size={18} className="spin"/>正在连接视频…</p>}
    </div>
    <Button variant="quiet" iconOnly className="icon-button online-player-switch" aria-label="使用 B站官方播放器" title="使用 B站官方播放器" onClick={() => { resume.current = getPosition() ?? resume.current; setEmbedded(true) }}><MonitorPlay size={16}/></Button>
  </div>
})
