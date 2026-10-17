import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { DiscoverySource } from './sources'
import type { Feed } from '../../shared/api/types'

const cacheKey = 'tangentry.discovery.v1'
function readCache(): Feed | null {
  try {
    const raw = sessionStorage.getItem(cacheKey)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Feed
    if (!Array.isArray(parsed.items) || typeof parsed.query !== 'string' || !Number.isInteger(parsed.page)) return null
    return parsed
  } catch { return null }
}

export function useFeed() {
  const [feed, setFeed] = useState<Feed | null>(readCache)
  const [source, updateSource] = useState<DiscoverySource>(() => (feed?.source as DiscoverySource) || 'bilibili')
  const lastSource = useRef<DiscoverySource>(source)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [lastQuery, setLastQuery] = useState(feed?.query ?? '')
  const controller = useRef<AbortController | null>(null)
  const requestID = useRef(0)
  const lastAppend = useRef(false)

  useEffect(() => {
    if (!feed) return
    try { sessionStorage.setItem(cacheKey, JSON.stringify(feed)) } catch { /* Browsing remains usable if storage is full or disabled. */ }
  }, [feed])
  useEffect(() => () => { controller.current?.abort() }, [])

  async function search(query: string, append = false, selected: DiscoverySource = source) {
    controller.current?.abort()
    controller.current = new AbortController()
    const id = ++requestID.current
    setLoading(true)
    setError('')
    setLastQuery(query)
    lastSource.current = selected
    const continuing = append && feed?.source === selected && feed.query.trim() === query.trim()
    lastAppend.current = Boolean(continuing)
    const page = continuing && feed ? feed.page + 1 : 1
    try {
      const result = await request<Feed>(`/discovery/${selected}?q=${encodeURIComponent(query.trim())}&page=${page}`, { signal: controller.current.signal })
      if (id !== requestID.current) return
      result.source = selected
      setFeed(current => {
        const retained = continuing && current?.source === selected && current.query.trim() === query.trim() ? current.items : []
        const seen = new Set(retained.map(item => item.id))
        const additions = result.items.filter(item => {
          if (seen.has(item.id)) return false
          seen.add(item.id)
          return true
        })
        return { ...result, items: [...retained, ...additions] }
      })
    } catch (error) {
      if (id !== requestID.current || (error instanceof DOMException && error.name === 'AbortError')) return
      setError(error instanceof Error ? error.message : '无法获取内容，请重试。')
    } finally { if (id === requestID.current) setLoading(false) }
  }

  function setSource(value: DiscoverySource) {
    controller.current?.abort()
    requestID.current++
    setLoading(false)
    setError('')
    updateSource(value)
  }

  return { feed, source, setSource, loading, error, search,
    accept: (item: import('../../shared/api/types').ContentItem) => { controller.current?.abort(); requestID.current++; setLoading(false); setError(''); setFeed({ source: 'pdf', items: [item], page: 1, total: 1, hasMore: false, sourceTitle: '本机 PDF', query: item.url }) },
    retry: () => search(lastQuery, lastAppend.current, lastSource.current) }
}
