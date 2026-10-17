import { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import { useReadingProgress } from './useReadingProgress'
import { ReadingProgressPanel } from './ReadingProgressPanel'
import { useSyncReaderPlayback } from '../../shared/reader/ReaderActions'

export function NativeReadingProgress({ contentId, media, identity }: {
  contentId: string; media: RefObject<HTMLMediaElement | null>; identity: string
}) {
  const progress = useReadingProgress(contentId)
  const [position, setPosition] = useState<number | null>(null)
  const [ready, setReady] = useState(false)
  const [playing, setPlaying] = useState(false)
  useSyncReaderPlayback(playing)
  const pendingSeek = useRef<number | null>(null)
  useEffect(() => {
    const node = media.current
    if (!node) return
    let engaged = false
    const report = (immediate = false) => {
      const valid = node.readyState >= 1 && !node.error && !node.seeking && (!(node instanceof HTMLVideoElement) || node.videoWidth > 0) && Number.isFinite(node.currentTime)
      setReady(valid)
      setPosition(valid ? node.currentTime : null)
      setPlaying(valid && !node.paused && !node.ended)
      if (valid && engaged) progress.observe(node.currentTime, node.duration, immediate)
    }
    const update = () => report()
    const checkpoint = () => report(true)
    const engage = () => { engaged = true; report(true) }
    const metadata = () => {
      const target = pendingSeek.current
      pendingSeek.current = null
      if (target !== null && Number.isFinite(node.duration) && target <= node.duration) node.currentTime = target
      report()
    }
    node.addEventListener('loadedmetadata', metadata)
    node.addEventListener('timeupdate', update)
    node.addEventListener('play', engage)
    node.addEventListener('seeking', engage)
    node.addEventListener('seeked', checkpoint)
    node.addEventListener('pause', checkpoint)
    node.addEventListener('ended', checkpoint)
    node.addEventListener('error', update)
    report()
    return () => {
      checkpoint()
      pendingSeek.current = null
      node.removeEventListener('loadedmetadata', metadata)
      node.removeEventListener('timeupdate', update)
      node.removeEventListener('play', engage)
      node.removeEventListener('seeking', engage)
      node.removeEventListener('seeked', checkpoint)
      node.removeEventListener('pause', checkpoint)
      node.removeEventListener('ended', checkpoint)
      node.removeEventListener('error', update)
    }
  }, [media, identity, progress.observe])
  const seek = (seconds: number) => {
    const node = media.current
    if (!node || node.error || !Number.isFinite(seconds) || seconds < 0) return
    if (node.readyState < 1) {
      pendingSeek.current = seconds
      node.preload = 'metadata'
      node.load()
      return
    }
    if (!Number.isFinite(node.duration) || seconds > node.duration) return
    node.currentTime = seconds
  }
  return <ReadingProgressPanel progress={progress} ready={ready || Boolean(media.current && !media.current.error)} playing={playing} position={position} onSeek={seek}/>
}
