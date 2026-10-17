import { useEffect, useId, useState } from 'react'
import { announcePlayback, mediaStartEvent } from '../../media/playback'

export interface GalleryCardPlayback {
  owner: string
  playing: boolean
  toggle: () => void
  setPlaying: (playing: boolean) => void
}

/** Keeps only one gallery audio item playing at a time. */
export function useGalleryPlayback(): GalleryCardPlayback {
  const owner = useId()
  const [playing, setPlaying] = useState(false)

  useEffect(() => {
    const stop = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== owner) setPlaying(false)
    }
    window.addEventListener(mediaStartEvent, stop)
    return () => window.removeEventListener(mediaStartEvent, stop)
  }, [owner])

  const toggle = () => {
    if (!playing) announcePlayback(owner)
    setPlaying(value => !value)
  }

  return { owner, playing, toggle, setPlaying }
}
