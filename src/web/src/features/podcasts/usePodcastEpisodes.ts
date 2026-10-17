import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SubscriptionInboxPage } from '../../shared/api/types'

// Latest audio entries that subscriptions have already fetched into the local inbox.
// The inbox accepts only its fixed page sizes (10, 20, 30, 50).
export function usePodcastEpisodes(active: boolean, limit = 10) {
  const [page, setPage] = useState<SubscriptionInboxPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const reload = useCallback(async () => {
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    setLoading(true); setError('')
    try {
      const params = new URLSearchParams({ q: '', sourceId: '', groupId: '', kind: 'audio', read: 'all', cursor: '', limit: String(limit) })
      const result = await request<SubscriptionInboxPage>('/subscription-inbox?' + params, { signal: current.signal })
      if (!current.signal.aborted) setPage(result)
    } catch (cause) {
      if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : '订阅单集暂时无法读取。')
    } finally {
      if (!current.signal.aborted) setLoading(false)
    }
  }, [limit])
  useEffect(() => {
    if (!active) return
    void reload()
    return () => controller.current?.abort()
  }, [active, reload])
  return { entries: page?.entries ?? [], loaded: page !== null, loading, error, reload }
}
