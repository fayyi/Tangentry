import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { DiscoveryBatchItem, DiscoveryBatchResponse, DiscoveryView } from '../../shared/api/types'

/** User-facing discovery controls. Deprecated fields remain optional for UI compatibility. */
export interface DiscoveryBatchOptions {
  view: DiscoveryView
  topic: string
  source: string
  /** @deprecated Batch size is selected by the server. */
  size?: 10 | 15 | 25
  /** @deprecated Subscription mix is selected by the server. */
  subscriptionRatio?: number
  /** @deprecated Exploration mix is selected by the server. */
  explorationRatio?: number
}

export interface DiscoveryBatchRequestParams {
  view: DiscoveryView
  topic?: string
  source?: string
  cursor?: string
  seed?: number
  unreadIds?: string[]
}

export const defaultDiscoveryBatchOptions: DiscoveryBatchOptions = {
  view: 'wander', topic: '', source: '', size: 15, subscriptionRatio: 60, explorationRatio: 15,
}

const sourceLabels: Record<string, string> = {
  subscriptions: '订阅更新', bilibili: 'B站推荐', youtube: 'YouTube 推荐',
  xiaohongshu: '小红书推荐', linuxdo: 'Linux.do 推荐', xiaoyuzhou: '小宇宙发现', douyin: '抖音推荐',
}

/** Build an optional topic value without sending empty strings. */
export function buildDiscoveryTopicParam(topic: string | undefined): string | undefined {
  const value = topic?.trim()
  return value || undefined
}

/** Build the request body; server defaults own size and source mix. */
export function buildDiscoveryBatchParams(
  options: Pick<DiscoveryBatchOptions, 'view' | 'topic' | 'source'>,
  cursor?: string,
  seed?: number,
  unreadIds: readonly string[] = [],
): DiscoveryBatchRequestParams {
  const params: DiscoveryBatchRequestParams = { view: options.view }
  const topic = buildDiscoveryTopicParam(options.topic)
  const source = options.source?.trim()
  if (topic) params.topic = topic
  if (source) params.source = source
  if (cursor) params.cursor = cursor
  if (seed !== undefined) params.seed = seed
  const ids = [...new Set(unreadIds.filter(Boolean))].slice(-100)
  if (ids.length) params.unreadIds = ids
  return params
}

/** Append a cursor page, preserving order and the first occurrence of an item. */
export function appendDiscoveryItems(current: readonly DiscoveryBatchItem[], next: readonly DiscoveryBatchItem[]): DiscoveryBatchItem[] {
  const seen = new Set<string>()
  const result: DiscoveryBatchItem[] = []
  for (const entry of [...current, ...next]) {
    if (seen.has(entry.item.id)) continue
    seen.add(entry.item.id)
    result.push(entry)
  }
  return result
}

/** Merge a cursor page into a visible response without mutating either response. */
export function appendDiscoveryBatch(current: DiscoveryBatchResponse | null, next: DiscoveryBatchResponse): DiscoveryBatchResponse {
  if (!current) return { ...next, items: [...next.items] }
  return {
    ...next,
    items: appendDiscoveryItems(current.items, next.items),
    sourceStatus: { ...current.sourceStatus, ...next.sourceStatus },
  }
}

/** Convert source/reason metadata into the quiet card label used by the feed. */
export function discoverySourceLabel(source: string, reason = '', carriedOver = false): string {
  if (carriedOver && source === 'subscriptions') return '订阅更新'
  if (/陌生探索|随机探索/.test(reason)) return '陌生探索'
  if (source === 'subscriptions' || /订阅优先|订阅更新/.test(reason)) return '订阅更新'
  return sourceLabels[source] ?? (source ? `${source} 推荐` : '发现')
}

export const getDiscoverySourceLabel = discoverySourceLabel

function randomSeed() { return Math.floor(Math.random() * 2147483647) || 1 }

export function useDiscoveryBatch(active: boolean) {
  const [options, setOptions] = useState<DiscoveryBatchOptions>(defaultDiscoveryBatchOptions)
  const [batch, setBatch] = useState<DiscoveryBatchResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [stopped, setStopped] = useState(false)
  const [topics, setTopics] = useState<string[]>(() => {
    try { return JSON.parse(localStorage.getItem('tangentry.discovery.topics') ?? '[]') as string[] } catch { return [] }
  })
  const optionsRef = useRef(options); optionsRef.current = options
  const batchRef = useRef<DiscoveryBatchResponse | null>(null); batchRef.current = batch
  const cursorRef = useRef<string | undefined>(undefined)
  const unreadIds = useRef<string[]>([])
  const controller = useRef<AbortController | null>(null)
  const seed = useRef(randomSeed())

  const load = useCallback(async (nextOptions: DiscoveryBatchOptions = optionsRef.current, cursor?: string) => {
    controller.current?.abort()
    const current = new AbortController(); controller.current = current
    setLoading(true); setError('')
    try {
      const params = buildDiscoveryBatchParams(nextOptions, cursor, seed.current, unreadIds.current)
      const result = await request<DiscoveryBatchResponse>('/discovery/batch', { method: 'POST', signal: current.signal, body: JSON.stringify(params) })
      if (current.signal.aborted) return
      const merged = cursor ? appendDiscoveryBatch(batchRef.current, result) : appendDiscoveryBatch(null, result)
      batchRef.current = merged; setBatch(merged)
      cursorRef.current = result.nextCursor
      setStopped(!result.hasNextBatch || !result.nextCursor)
    } catch (cause) {
      if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : '发现内容暂不可用，请重试。')
    } finally {
      if (!current.signal.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!active) return
    void load(optionsRef.current)
    return () => controller.current?.abort()
  }, [active, load])

  const update = useCallback((patch: Partial<DiscoveryBatchOptions>) => {
    const next = { ...optionsRef.current, ...patch }
    unreadIds.current = batchRef.current?.items.map(item => item.item.id) ?? []
    optionsRef.current = next; setOptions(next); seed.current = randomSeed()
    cursorRef.current = undefined; batchRef.current = null; setBatch(null); setStopped(false)
    if (active) void load(next)
  }, [active, load])

  const nextBatch = useCallback(() => {
    const current = batchRef.current; const cursor = cursorRef.current ?? current?.nextCursor
    if (!cursor || loading || stopped) return
    unreadIds.current = current?.items.map(item => item.item.id) ?? []
    void load(optionsRef.current, cursor)
  }, [load, loading, stopped])

  const retry = useCallback(() => {
    const current = batchRef.current; const cursor = cursorRef.current ?? current?.nextCursor
    void load(optionsRef.current, cursor)
  }, [load])
  const reset = useCallback(() => update(defaultDiscoveryBatchOptions), [update])
  const saveTopics = useCallback((next: string[]) => {
    const unique = [...new Set(next.map(value => value.trim()).filter(Boolean))].slice(0, 20)
    setTopics(unique)
    try { localStorage.setItem('tangentry.discovery.topics', JSON.stringify(unique)) } catch { /* Preferences remain usable without storage. */ }
  }, [])
  const addTopic = useCallback((topic: string) => saveTopics([...topics, topic]), [saveTopics, topics])
  const removeTopic = useCallback((topic: string) => saveTopics(topics.filter(value => value !== topic)), [saveTopics, topics])

  return {
    options, batch, items: batch?.items ?? [], hasMore: Boolean(batch?.hasNextBatch && batch.nextCursor),
    loading, error, stopped, topics, addTopic, removeTopic, setOptions: update, nextBatch, retry, reset,
  }
}
