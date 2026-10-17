import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { DiscoveryTopicRule } from '../../shared/api/types'

export function useDiscoveryTopics(active: boolean) {
  const [topics, setTopics] = useState<DiscoveryTopicRule[]>([])
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const reload = useCallback(async () => {
    controller.current?.abort()
    const next = new AbortController(); controller.current = next
    try {
      const result = await request<{ topics: DiscoveryTopicRule[] }>('/discovery/topics', { signal: next.signal })
      if (!next.signal.aborted) { setTopics(result.topics); setError('') }
    } catch (cause) { if (!next.signal.aborted) setError(cause instanceof Error ? cause.message : '主题规则暂不可用。') }
  }, [])
  const create = useCallback(async (name: string) => {
    const clean = name.trim()
    if (!clean) return false
    try {
      const result = await request<DiscoveryTopicRule>('/discovery/topics', { method: 'POST', body: JSON.stringify({ name: clean, keywords: [clean] }) })
      setTopics(current => [...current.filter(topic => topic.id !== result.id), result])
      return true
    } catch (cause) { setError(cause instanceof Error ? cause.message : '主题保存失败。'); return false }
  }, [])
  const update = useCallback(async (topic: DiscoveryTopicRule, name: string) => {
    const clean = name.trim()
    if (!clean) return false
    try {
      const result = await request<DiscoveryTopicRule>(`/discovery/topics/${encodeURIComponent(topic.id)}`, { method: 'PUT', body: JSON.stringify({ name: clean, keywords: [clean], revision: topic.revision }) })
      setTopics(current => current.map(value => value.id === result.id ? result : value))
      return true
    } catch (cause) { setError(cause instanceof Error ? cause.message : '主题保存失败。'); return false }
  }, [])
  const remove = useCallback(async (topic: DiscoveryTopicRule) => {
    try {
      await request<void>(`/discovery/topics/${encodeURIComponent(topic.id)}?revision=${topic.revision}`, { method: 'DELETE' })
      setTopics(current => current.filter(value => value.id !== topic.id))
      return true
    } catch (cause) { setError(cause instanceof Error ? cause.message : '主题删除失败。'); return false }
  }, [])
  useEffect(() => { if (active) void reload(); return () => controller.current?.abort() }, [active, reload])
  return { topics, error, reload, create, update, remove }
}
