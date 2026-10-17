import { useEffect, useLayoutEffect, useRef } from 'react'
import type { RefObject } from 'react'

// Read the media clock, never extrapolate it: buffering and seeks must remain
// truthful. Poll only while visible playback is advancing.
export function useNativePlaybackClock(media: RefObject<HTMLMediaElement | null>, identity: unknown, report?: (seconds: number | null) => void) {
  const callback = useRef(report)
  useLayoutEffect(() => { callback.current = report }, [report])
  useEffect(() => {
    const node = media.current
    if (!node) return
    let timer: number | undefined
    const sample = () => {
      if (!node.seeking && node.readyState >= 1 && !node.error && (!(node instanceof HTMLVideoElement) || node.videoWidth > 0) && Number.isFinite(node.currentTime)) callback.current?.(node.currentTime)
    }
    const stop = () => { window.clearTimeout(timer); timer = undefined }
    const tick = () => {
      stop()
      if (document.hidden || node.paused || node.ended || node.error) return
      sample()
      timer = window.setTimeout(tick, 100)
    }
    const checkpoint = () => { stop(); sample(); tick() }
    const seeking = () => { stop(); callback.current?.(null) }
    const visibility = () => { if (document.hidden) stop(); else checkpoint() }
    node.addEventListener('playing', tick)
    node.addEventListener('pause', checkpoint)
    node.addEventListener('ended', checkpoint)
    node.addEventListener('seeked', checkpoint)
    node.addEventListener('seeking', seeking)
    document.addEventListener('visibilitychange', visibility)
    tick()
    return () => {
      stop()
      node.removeEventListener('playing', tick)
      node.removeEventListener('pause', checkpoint)
      node.removeEventListener('ended', checkpoint)
      node.removeEventListener('seeked', checkpoint)
      node.removeEventListener('seeking', seeking)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [media, identity])
}
