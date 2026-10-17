import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem, Feed } from '../../shared/api/types'
import { useRecommendationPreferences } from '../settings/useRecommendationPreferences'
import { recommendationRequests } from './recommendationRequests'

export const gallerySources = ['subscriptions', 'bilibili', 'youtube', 'xiaohongshu', 'linuxdo', 'xiaoyuzhou', 'douyin'] as const
export type GallerySource = typeof gallerySources[number]
type State = { feed: Feed | null; loading: boolean; error: string; known: boolean; exhausted: boolean }
const initial = (): State => ({feed:null,loading:false,error:'',known:false,exhausted:false})
export function useGallery(active: boolean, scope: readonly GallerySource[] = gallerySources) {
  const preferences = useRecommendationPreferences()
  const enabled = useRef(preferences.values); enabled.current = preferences.values
  const [sources,setSources] = useState<Record<GallerySource,State>>({subscriptions:initial(),bilibili:initial(),youtube:initial(),xiaohongshu:initial(),linuxdo:initial(),xiaoyuzhou:initial(),douyin:initial()})
  const current = useRef(sources);current.current=sources
  const controllers = useRef<Partial<Record<GallerySource,AbortController>>>({})
  const lastAppend = useRef<Partial<Record<GallerySource, boolean>>>({})
  const recommendationSessions = useRef<Partial<Record<GallerySource, string>>>({})
  const inventory = useRef<Partial<Record<GallerySource, string>>>({})
  const previousEnabled = useRef(preferences.values)
  const acquire = useRef(recommendationRequests())
  const read = useCallback(async (source: GallerySource, append = false, force = false) => {
    if (source !== 'subscriptions' && !enabled.current?.[source]) return false
    const remoteEnabled = () => source === 'subscriptions' || Boolean(enabled.current?.[source])
    if (append && (current.current[source].loading || controllers.current[source] && !controllers.current[source]?.signal.aborted || !force && current.current[source].exhausted)) return false
    controllers.current[source]?.abort()
    const controller = new AbortController();controllers.current[source]=controller
    lastAppend.current[source] = append
    const usesSession = source === 'youtube' || source === 'xiaoyuzhou'
    const session = usesSession ? (append ? recommendationSessions.current[source] || '' : recommendationSessions.current[source] || crypto.randomUUID()) : ''
    if (usesSession && !append) recommendationSessions.current[source] = session
    setSources(previous=>({...previous,[source]:{...previous[source],loading:true,error:''}}))
    let accumulated = append ? current.current[source].feed : null
    let timeout: number | undefined
    let release: (() => void) | undefined
    const startTimeout = () => window.setTimeout(() => {
      if (controllers.current[source] !== controller || controller.signal.aborted) return
      controller.abort()
      setSources(previous => ({ ...previous, [source]: { ...previous[source], feed: accumulated ?? previous[source].feed,
        loading: false, known: true, error: '推荐获取超过 30 秒，其他来源仍可浏览，请稍后重试。' } }))
    }, 30000)
    timeout = startTimeout()
    try {
      if ((!append && (!force || !remoteEnabled()) || append && inventory.current[source]) && source !== 'subscriptions') {
        const cursor = append ? inventory.current[source] : ''
        const cached = await request<{ items: { item: ContentItem }[]; next?: string }>(`/recommendations/${source}/pending?limit=60${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, { signal: controller.signal })
          .catch(cause => { if (controller.signal.aborted || append) throw cause; return { items: [], next: '' } })
        if (controller.signal.aborted) return false
        inventory.current[source] = cached.next || undefined
        if (cached.items.length) {
          const existing = new Set(accumulated?.items.map(item => item.id) ?? [])
          const items = [...(accumulated?.items ?? []), ...cached.items.map(entry => entry.item).filter(item => !existing.has(item.id))]
          const hasMore = remoteEnabled() || Boolean(cached.next)
          const feed: Feed = { items, page: 0, total: items.length, hasMore, sourceTitle: '', query: '' }
          setSources(previous => ({ ...previous, [source]: { feed, loading: false, error: '', known: true, exhausted: !hasMore } }))
          return hasMore
        }
      }
      if (!remoteEnabled()) {
        setSources(previous => ({ ...previous, [source]: { ...previous[source], feed: accumulated ? { ...accumulated, hasMore: false } : previous[source].feed, loading: false, error: '', known: true, exhausted: true } }))
        return false
      }
      window.clearTimeout(timeout)
      release = await acquire.current(controller.signal)
      if (controller.signal.aborted || !remoteEnabled()) return false
      timeout = startTimeout()
      if (!append) inventory.current[source] = undefined
      for (let attempt = 0; attempt < (append ? 3 : 1); attempt++) {
        const page = append ? (accumulated?.page ?? 0) + 1 : 1
        const endpoint = append ? `/recommendations/${source}?page=${page}` : `/recommendations/${source}`
        const feed=await request<Feed>(endpoint,{signal:controller.signal, ...(session ? { headers: { 'X-Recommendation-Session': session } } : {})})
        if(controller.signal.aborted) return false
        if (usesSession) recommendationSessions.current[source] = session
        const existing = new Set(accumulated?.items.map(item => item.id) ?? [])
        const additions = feed.items.filter(item => {
          if (existing.has(item.id)) return false
          existing.add(item.id)
          return true
        })
        accumulated = { ...feed, items: [...(accumulated?.items ?? []), ...additions], total: Math.max(feed.total, accumulated?.total ?? 0), page }
        if (append && additions.length === 0 && feed.hasMore && attempt < 2) continue
        const exhausted = !feed.hasMore || additions.length === 0
        setSources(previous=>({...previous,[source]:{feed:accumulated,loading:false,error:'',known:true,exhausted}}))
        return !exhausted
      }
    } catch(cause) {
      if(!controller.signal.aborted)setSources(previous=>({...previous,[source]:{...previous[source],feed:accumulated ?? previous[source].feed,loading:false,known:true,error:cause instanceof Error?cause.message:'来源暂不可用。'}}))
    } finally {
      window.clearTimeout(timeout)
      release?.()
      if (controllers.current[source] === controller) delete controllers.current[source]
    }
    return false
  },[])
  useEffect(() => {
    if (!preferences.values) return
    const before = previousEnabled.current
    previousEnabled.current = preferences.values
    for (const source of gallerySources) {
      if (source === 'subscriptions' || preferences.values[source]) continue
      controllers.current[source]?.abort()
      delete recommendationSessions.current[source]
      delete inventory.current[source]
    }
    setSources(previous => Object.fromEntries(gallerySources.map(source => {
      if (source === 'subscriptions' || preferences.values?.[source]) return [source, previous[source]]
      return [source, { ...initial(), known: true, exhausted: true }]
    })) as Record<GallerySource, State>)
    for (const source of gallerySources) {
      if (source !== 'subscriptions' && before && !before[source] && preferences.values[source]) {
        if (active && scope.includes(source)) void read(source, false, true)
        else setSources(previous => ({ ...previous, [source]: { ...previous[source], known: false, exhausted: false } }))
      }
    }
  }, [preferences.values, active, read, scope])
  useEffect(()=>{
    if(!active)return
    if (!preferences.values && !preferences.error) return
    if (!preferences.values) {
      setSources(previous => Object.fromEntries(gallerySources.map(source => [source, source === 'subscriptions' ? previous[source] : { ...previous[source], loading: false, known: true, error: '推荐设置读取失败，请在设置中重试。' }])) as Record<GallerySource, State>)
      if (scope.includes('subscriptions')) void read('subscriptions')
      return
    }
    for(const source of scope)if(source==='subscriptions'||!current.current[source].known)void read(source)
  },[active,read,preferences.values,preferences.error,scope])
  useEffect(() => {
    if (!active) return
    return () => {
      for (const source of gallerySources) controllers.current[source]?.abort()
      setSources(previous => Object.fromEntries(gallerySources.map(source => [source, { ...previous[source], loading: false }])) as Record<GallerySource, State>)
    }
  }, [active])
  const refresh = (selected: readonly GallerySource[] = gallerySources) => {for(const source of selected)if(source === 'subscriptions' || enabled.current?.[source])void read(source, false, true)}
  const loadMore = useCallback(async (selected: readonly GallerySource[] = gallerySources.filter(source => source !== 'subscriptions')) => {
    await Promise.all(selected.filter(source => !current.current[source].error && current.current[source].feed?.hasMore).map(source => read(source, true)))
  }, [read])
  const retry = useCallback((source: GallerySource) => read(source,
    current.current[source].error ? Boolean(lastAppend.current[source]) : Boolean(current.current[source].feed?.hasMore), true), [read])
  const accept = useCallback((source: GallerySource, feed: Feed) => {
    inventory.current[source] = undefined
    delete recommendationSessions.current[source]
    controllers.current[source]?.abort()
    setSources(previous => ({ ...previous, [source]: { feed, loading: false, error: '', known: true, exhausted: !feed.hasMore } }))
  }, [])
  const invalidate = useCallback((platform: string) => {
    if (platform !== 'bilibili' && platform !== 'youtube' && platform !== 'xiaohongshu' && platform !== 'linuxdo' && platform !== 'xiaoyuzhou' && platform !== 'douyin') return
    delete recommendationSessions.current[platform]
    inventory.current[platform] = undefined
    controllers.current[platform]?.abort()
    const isEnabled = Boolean(enabled.current?.[platform])
    setSources(previous => ({ ...previous, [platform]: { ...initial(), known: !isEnabled, exhausted: !isEnabled } }))
  }, [])
  return {sources,read,refresh,loadMore,retry,accept,invalidate,enabled:preferences.values}
}
