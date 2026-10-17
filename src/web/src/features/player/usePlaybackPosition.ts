import { useCallback, useEffect, useRef, useState } from 'react'

// Media elements can emit timeupdate much faster than the transcript needs.
// Keep the UI clock smooth at 10 fps while avoiding a full reader-tree render
// for every native event.
export function usePlaybackPosition() {
  const [position, setPosition] = useState<number | null>(null)
  const latest = useRef<number | null>(null)
  const painted = useRef<number | null>(null)
  const timer = useRef<number | null>(null)
  const lastPaint = useRef(0)
  const flush = useCallback(() => {
    timer.current = null
    if (latest.current !== painted.current) {
      painted.current = latest.current
      lastPaint.current = performance.now()
      setPosition(latest.current)
    }
  }, [])
  const report = useCallback((value: number | null) => {
    latest.current = value !== null && Number.isFinite(value) ? value : null
    if (latest.current === null) {
      if (timer.current !== null) window.clearTimeout(timer.current)
      timer.current = null
      flush()
      return
    }
    const delta = painted.current === null ? Infinity : Math.abs(latest.current - painted.current)
    // Seeks and cue changes should be reflected immediately; ordinary playback
    // advances are painted at 10 fps to keep article mode calm.
    if (delta >= 0.5) {
      if (timer.current !== null) window.clearTimeout(timer.current)
      timer.current = null
      flush()
    } else if (delta > 0) {
      if (timer.current === null) timer.current = window.setTimeout(flush, Math.max(0, 100 - (performance.now() - lastPaint.current)))
    }
  }, [flush])
  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current) }, [])
  return [position, report] as const
}
