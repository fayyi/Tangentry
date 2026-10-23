import { Button } from '../../components/ui'
import { useEffect, useRef, useState } from 'react'
import { Volume2, VolumeX } from 'lucide-react'
import { loadYouTubeAPI, type YouTubePlayer } from './youtubeIframeAPI'

export function YouTubeHoverPreview({ id }: { id: string }) {
  const container = useRef<HTMLSpanElement>(null)
  const player = useRef<YouTubePlayer | null>(null)
  const [ready, setReady] = useState(false)
  const [muted, setMuted] = useState(false)
  useEffect(() => {
    const host = container.current
    if (!host || !/^[A-Za-z0-9_-]{11}$/.test(id)) return
    let active = true
    const frame = document.createElement('iframe')
    frame.className = 'hover-video-preview'
    frame.title = 'YouTube 视频预览'
    frame.src = `https://www.youtube-nocookie.com/embed/${id}?autoplay=1&mute=0&controls=1&start=5&playsinline=1&cc_load_policy=1&cc_lang_pref=zh-Hans&enablejsapi=1&origin=${encodeURIComponent(location.origin)}`
    frame.allow = 'autoplay; encrypted-media; picture-in-picture'
    frame.referrerPolicy = 'strict-origin-when-cross-origin'
    host.append(frame)
    setReady(false); setMuted(false)
    void loadYouTubeAPI().then(api => {
      if (!active) return
      player.current = new api.Player(frame, { events: {
        onReady: () => {
          if (!active) return
          try { player.current?.unMute?.(); setMuted(player.current?.isMuted?.() ?? false) } catch { setReady(false); return }
          setReady(Boolean(player.current?.mute && player.current?.unMute && player.current?.isMuted))
        },
        onError: () => { if (active) setReady(false) },
        onAutoplayBlocked: () => {
          if (!active) return
          try { player.current?.mute?.(); setMuted(player.current?.isMuted?.() ?? true); player.current?.playVideo?.() } catch { setReady(false) }
        },
        onStateChange: () => {
          if (!active) return
          try { if (player.current?.isMuted) setMuted(player.current.isMuted()) } catch { setReady(false) }
        },
      } })
    }).catch(() => { if (active) setReady(false) })
    return () => {
      active = false
      try { player.current?.destroy() } catch { /* Removing the iframe also stops a failed API player. */ }
      finally { player.current = null; frame.src = 'about:blank'; host.replaceChildren() }
    }
  }, [id])
  function toggleSound() {
    try {
      const current = player.current
      if (!current?.isMuted || !current.mute || !current.unMute) return
      if (current.isMuted()) { current.unMute(); if (current.getPlayerState?.() !== 1) current.playVideo?.() }
      else current.mute()
      setMuted(current.isMuted())
    } catch { setReady(false) }
  }
  return <span style={{ position: 'absolute', inset: 0 }}>
    <span ref={container}/>
    {ready && <Button variant="quiet" type="button" className="preview-sound" title={muted ? '开启预览声音' : '静音预览'} aria-label={muted ? '开启预览声音' : '静音预览'} onClick={event => { event.stopPropagation(); toggleSound() }}>{muted ? <VolumeX size={16}/> : <Volume2 size={16}/>}</Button>}
  </span>
}
