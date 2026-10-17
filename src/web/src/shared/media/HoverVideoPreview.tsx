import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../api/client'
import type { ContentItem } from '../api/types'
import { mediaStartEvent } from './playback'
import { Volume2, VolumeX } from 'lucide-react'
import { YouTubeHoverPreview } from './YouTubeHoverPreview'

export function HoverVideoPreview({ item, active }: { item: ContentItem; active: boolean }) {
  const [url, setURL] = useState('')
  const [embedded, setEmbedded] = useState(false)
  const [muted, setMuted] = useState(false)
  const video = useRef<HTMLVideoElement>(null)
  const attachVideo = useCallback((node: HTMLVideoElement | null) => {
    if (!node) video.current?.pause()
    video.current = node
  }, [])
  const host = useRef<HTMLSpanElement>(null)
  const [inViewport, setInViewport] = useState(true)
  const [interrupted, setInterrupted] = useState(false)
  useEffect(() => {
    setInterrupted(false)
    setMuted(false)
  }, [active])
  useEffect(() => {
    const stop = () => setInterrupted(true)
    window.addEventListener(mediaStartEvent, stop)
    return () => window.removeEventListener(mediaStartEvent, stop)
  }, [])
  useEffect(() => {
    if (!host.current) return
    const observer = new IntersectionObserver(entries => setInViewport(entries[0]?.isIntersecting ?? false))
    observer.observe(host.current)
    return () => observer.disconnect()
  }, [])
  const [visible, setVisible] = useState(() => !document.hidden)
  useEffect(() => {
    const update = () => setVisible(!document.hidden)
    document.addEventListener('visibilitychange', update)
    return () => document.removeEventListener('visibilitychange', update)
  }, [])
  useEffect(() => {
    if (!active || !visible || !inViewport || interrupted) setMuted(false)
  }, [active, visible, inViewport, interrupted])
  useEffect(() => {
    if (!active || !visible || !inViewport || interrupted) return
    const controller = new AbortController()
    let alive = true, token = ''
    const timer = setTimeout(() => {
      if (item.platform === 'bilibili') {
        void request<{ id: string }>(`/playback/bilibili/${item.platformId}`, { method: 'POST', signal: controller.signal }).then(session => {
          token = session.id
          if (alive) setURL(`/api/playback/sessions/${token}/media`)
          else void request(`/playback/sessions/${token}`, { method: 'DELETE', keepalive: true }).catch(() => {})
        }).catch(() => { if (alive && !controller.signal.aborted) setEmbedded(true) })
      } else if (item.platform === 'youtube') setEmbedded(true)
      else if (item.mediaUrl) setURL(item.mediaUrl)
    }, 650)
    return () => {
      alive = false; clearTimeout(timer); controller.abort(); setURL(''); setEmbedded(false)
      const node = video.current; node?.pause(); if (node) { node.removeAttribute('src'); node.load() }
      if (token) void request(`/playback/sessions/${token}`, { method: 'DELETE', keepalive: true }).catch(() => {})
    }
  }, [active, visible, inViewport, interrupted, item.platform, item.platformId, item.mediaUrl])
  let preview = null
  if (active && visible && inViewport && !interrupted && embedded) preview = item.platform === 'youtube' ? <YouTubeHoverPreview id={item.platformId}/> : <iframe className="hover-video-preview" title="B站视频预览" src={`https://player.bilibili.com/player.html?bvid=${item.platformId}&autoplay=1&muted=0&danmaku=0&t=5`}
    allow="autoplay; encrypted-media" referrerPolicy="strict-origin-when-cross-origin"/>
  else if (active && visible && inViewport && !interrupted && url) preview = <video ref={attachVideo} className="hover-video-preview" aria-label="视频预览" src={url} muted={muted} controls playsInline preload="metadata"
    onLoadedMetadata={event => {
      const node = event.currentTarget
      node.currentTime = Math.min(5, node.duration / 4)
      void node.play().catch(error => {
        if (error?.name !== 'NotAllowedError' || video.current !== node || !node.isConnected) return
        node.muted = true; setMuted(true)
        void node.play().catch(() => {})
      })
    }}
    onVolumeChange={() => { if (video.current) setMuted(video.current.muted) }}
    onError={() => { setURL(''); if (item.platform === 'bilibili') setEmbedded(true) }}/>
  return <span ref={host} style={{ position: 'absolute', inset: 0, pointerEvents: preview ? 'auto' : 'none', zIndex: preview ? 4 : undefined }}>{preview}{preview && !embedded && <button type="button" className="preview-sound" title={muted ? '开启预览声音' : '静音预览'} aria-label={muted ? '开启预览声音' : '静音预览'} onClick={event => {
    event.stopPropagation()
    const node = video.current
    if (!node) return
    node.muted = !node.muted; setMuted(node.muted)
    if (!node.muted && node.paused) void node.play().catch(() => {})
  }}>{muted ? <VolumeX size={16}/> : <Volume2 size={16}/>}</button>}</span>
}
