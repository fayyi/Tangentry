import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'

export type YouTubeChannel = { channelId: string; title: string; url: string }

export function useYouTubeChannelResolution() {
  const [channel, setChannel] = useState<YouTubeChannel | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const reset = () => { controller.current?.abort(); controller.current = null; setChannel(null); setError(''); setLoading(false) }
  useEffect(() => () => controller.current?.abort(), [])

  async function resolve(query: string) {
    reset()
    const active = new AbortController()
    controller.current = active
    setLoading(true)
    try {
      const result = await request<YouTubeChannel>('/subscriptions/youtube/resolve', { method: 'POST', body: JSON.stringify({ query }), signal: active.signal })
      if (!active.signal.aborted) setChannel(result)
    } catch (cause) {
      if (!active.signal.aborted) setError(cause instanceof Error ? cause.message : '频道暂时无法解析，请稍后重试。')
    } finally {
      if (!active.signal.aborted) { setLoading(false); controller.current = null }
    }
  }
  return { channel, loading, error, resolve, reset }
}
