import { useCallback, useEffect, useState } from 'react'
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

export const discoverySessionStaleAfterMs = 30 * 60 * 1000

export interface DiscoveryBatchSession {
  options: DiscoveryBatchOptions
  batch: DiscoveryBatchResponse | null
  cursor?: string
  seed: number
  unreadIds: string[]
  stopped: boolean
  loading: boolean
  lastLoadedAt: number
  requestID: number
  controller?: AbortController
}

export interface DiscoveryFilterState {
  kind: string
  platform: string
  topic: string
}

/** Pure session helpers kept small so the cache policy can be tested without React. */
export function createDiscoveryBatchSession(options: DiscoveryBatchOptions = defaultDiscoveryBatchOptions, seed = 1): DiscoveryBatchSession {
  return {
    options: { ...options }, batch: null, cursor: undefined, seed, unreadIds: [], stopped: false,
    loading: false, lastLoadedAt: 0, requestID: 0,
  }
}

export function isDiscoverySessionStale(lastLoadedAt: number, now = Date.now(), maxAge = discoverySessionStaleAfterMs): boolean {
  return lastLoadedAt > 0 && now - lastLoadedAt >= maxAge
}

export function restoreDiscoveryBatchSession(session: DiscoveryBatchSession | null): DiscoveryBatchSession | null {
  if (!session) return null
  return {
    ...session,
    options: { ...session.options },
    batch: session.batch ? { ...session.batch, items: [...session.batch.items], sourceStatus: { ...session.batch.sourceStatus } } : null,
    unreadIds: [...session.unreadIds],
  }
}

const sourceLabels: Record<string, string> = {
  subscriptions: '订阅更新', bilibili: 'B站推荐', youtube: 'YouTube 推荐',
  xiaohongshu: '小红书推荐', linuxdo: 'Linux.do 推荐', xiaoyuzhou: '小宇宙发现', douyin: '抖音推荐',
  huggingface: 'HF 每日论文',
}

const discoverySession = createDiscoveryBatchSession(defaultDiscoveryBatchOptions, randomSeed())
const discoveryFilters: DiscoveryFilterState = { kind: 'all', platform: 'all', topic: '' }
const discoveryListeners = new Set<() => void>()

function notifyDiscoverySession() { discoveryListeners.forEach(listener => listener()) }

function subscribeDiscoverySession(listener: () => void) {
  discoveryListeners.add(listener)
  return () => { discoveryListeners.delete(listener) }
}

export function readDiscoveryFilterState(): DiscoveryFilterState { return { ...discoveryFilters } }

export function updateDiscoveryFilterState(patch: Partial<DiscoveryFilterState>): DiscoveryFilterState {
  Object.assign(discoveryFilters, patch)
  return readDiscoveryFilterState()
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

/** Classify the backend reason into the compact discovery badge. */
export function discoveryReasonType(source: string, reason = '', carriedOver = false): '订阅' | '推荐' | '探索' {
  if (carriedOver && source === 'subscriptions') return '订阅'
  if (source === 'subscriptions' || /订阅优先|订阅更新/.test(reason)) return '订阅'
  if (/陌生探索|随机探索/.test(reason)) return '探索'
  return '推荐'
}

export const getDiscoverySourceLabel = discoverySourceLabel

function randomSeed() { return Math.floor(Math.random() * 2147483647) || 1 }

export function useDiscoveryBatch(active: boolean) {
  const [, forceUpdate] = useState(0)
  const [error, setError] = useState('')
  const [topics, setTopics] = useState<string[]>(() => {
    try { return JSON.parse(localStorage.getItem('tangentry.discovery.topics') ?? '[]') as string[] } catch { return [] }
  })
  useEffect(() => subscribeDiscoverySession(() => forceUpdate(value => value + 1)), [])

  const load = useCallback(async (nextOptions: DiscoveryBatchOptions = discoverySession.options, cursor?: string) => {
    discoverySession.controller?.abort()
    const current = new AbortController()
    discoverySession.controller = current
    const requestID = ++discoverySession.requestID
    discoverySession.loading = true
    notifyDiscoverySession()
    setError('')
    try {
      const params = buildDiscoveryBatchParams(nextOptions, cursor, discoverySession.seed, discoverySession.unreadIds)
      const result = await request<DiscoveryBatchResponse>('/discovery/batch', { method: 'POST', signal: current.signal, body: JSON.stringify(params) })
      if (current.signal.aborted || requestID !== discoverySession.requestID) return
      const merged = cursor ? appendDiscoveryBatch(discoverySession.batch, result) : appendDiscoveryBatch(null, result)
      discoverySession.batch = merged
      discoverySession.cursor = result.nextCursor
      discoverySession.stopped = !result.hasNextBatch || !result.nextCursor
      discoverySession.options = { ...nextOptions }
      discoverySession.lastLoadedAt = Date.now()
    } catch (cause) {
      if (!current.signal.aborted && requestID === discoverySession.requestID) setError(cause instanceof Error ? cause.message : '发现内容暂不可用，请重试。')
    } finally {
      if (!current.signal.aborted && requestID === discoverySession.requestID) {
        discoverySession.loading = false
        discoverySession.controller = undefined
        notifyDiscoverySession()
      }
    }
  }, [])

  useEffect(() => {
    if (!active) return
    if (!discoverySession.batch && !discoverySession.loading) void load(discoverySession.options)
  }, [active, load])

  const update = useCallback((patch: Partial<DiscoveryBatchOptions>) => {
    const next = { ...discoverySession.options, ...patch }
    discoverySession.unreadIds = discoverySession.batch?.items.map(item => item.item.id) ?? []
    discoverySession.options = next
    discoverySession.seed = randomSeed()
    discoverySession.cursor = undefined
    discoverySession.batch = null
    discoverySession.stopped = false
    discoverySession.lastLoadedAt = 0
    notifyDiscoverySession()
    if (active) void load(next)
  }, [active, load])

  const nextBatch = useCallback(() => {
    const current = discoverySession.batch; const cursor = discoverySession.cursor ?? current?.nextCursor
    if (!cursor || discoverySession.loading || discoverySession.stopped) return
    discoverySession.unreadIds = current?.items.map(item => item.item.id) ?? []
    void load(discoverySession.options, cursor)
  }, [load])

  const retry = useCallback(() => {
    const current = discoverySession.batch
    discoverySession.cursor = undefined
    discoverySession.stopped = false
    discoverySession.unreadIds = current?.items.map(item => item.item.id) ?? []
    void load(discoverySession.options)
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
    options: discoverySession.options, batch: discoverySession.batch, items: discoverySession.batch?.items ?? [], hasMore: Boolean(discoverySession.batch?.hasNextBatch && discoverySession.batch.nextCursor),
    loading: discoverySession.loading, error, stopped: discoverySession.stopped, lastLoadedAt: discoverySession.lastLoadedAt,
    optionsStale: isDiscoverySessionStale(discoverySession.lastLoadedAt), topics, addTopic, removeTopic, setOptions: update, nextBatch, retry, reset,
  }
}
