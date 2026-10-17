import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { DiscoveryBatchResponse, DiscoveryView } from '../../shared/api/types'

export interface DiscoveryBatchOptions {
  view: DiscoveryView
  size: 10 | 15 | 25
  topic: string
  source: string
  subscriptionRatio: number
  explorationRatio: number
}

const defaultOptions: DiscoveryBatchOptions = { view: 'wander', size: 15, topic: '', source: '', subscriptionRatio: 60, explorationRatio: 15 }

export function useDiscoveryBatch(active: boolean) {
  const [options, setOptions] = useState<DiscoveryBatchOptions>(defaultOptions)
  const [batch, setBatch] = useState<DiscoveryBatchResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [stopped, setStopped] = useState(false)
  const [topics, setTopics] = useState<string[]>(() => {
    try { return JSON.parse(localStorage.getItem('tangentry.discovery.topics') ?? '[]') as string[] } catch { return [] }
  })
  const unreadIds = useRef<string[]>([])
  const controller = useRef<AbortController | null>(null)
  const seed = useRef(Math.floor(Math.random() * 2147483647) || 1)

  const load = useCallback(async (nextOptions = options, cursor?: string) => {
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    setLoading(true)
    setError('')
    try {
      const result = await request<DiscoveryBatchResponse>('/discovery/batch', {
        method: 'POST', signal: current.signal, body: JSON.stringify({ ...nextOptions, topic: nextOptions.topic.trim() || undefined, cursor, seed: seed.current, unreadIds: unreadIds.current }),
      })
      if (current.signal.aborted) return
      setBatch(currentBatch => cursor && currentBatch
        ? { ...result, items: [...currentBatch.items, ...result.items] }
        : result)
      setStopped(!result.hasNextBatch)
    } catch (cause) {
      if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : '发现批次暂不可用，请重试。')
    } finally {
      if (!current.signal.aborted) setLoading(false)
    }
  }, [options])

  useEffect(() => {
    if (!active) return
    void load()
    return () => controller.current?.abort()
  }, [active]) // New view activation starts exactly one finite batch.

  const update = useCallback((patch: Partial<DiscoveryBatchOptions>) => {
    const next = { ...options, ...patch }
    unreadIds.current = batch?.items.map(item => item.item.id) ?? []
    setOptions(next)
    seed.current = Math.floor(Math.random() * 2147483647) || 1
    setBatch(null)
    setStopped(false)
    if (active) void load(next)
  }, [active, batch, load, options])

  const nextBatch = useCallback(() => {
    if (!batch?.nextCursor || loading) return
    unreadIds.current = batch.items.map(item => item.item.id)
    void load(options, batch.nextCursor)
  }, [batch, load, loading, options])

  const retry = useCallback(() => { void load(options, batch?.nextCursor) }, [batch?.nextCursor, load, options])
  const reset = useCallback(() => update(defaultOptions), [update])
  const saveTopics = useCallback((next: string[]) => {
    const unique = [...new Set(next.map(value => value.trim()).filter(Boolean))].slice(0, 20)
    setTopics(unique)
    try { localStorage.setItem('tangentry.discovery.topics', JSON.stringify(unique)) } catch { /* Preferences remain usable without storage. */ }
  }, [])
  const addTopic = useCallback((topic: string) => saveTopics([...topics, topic]), [saveTopics, topics])
  const removeTopic = useCallback((topic: string) => saveTopics(topics.filter(value => value !== topic)), [saveTopics, topics])

  return { options, batch, loading, error, stopped, topics, addTopic, removeTopic, setOptions: update, nextBatch, retry, reset }
}
