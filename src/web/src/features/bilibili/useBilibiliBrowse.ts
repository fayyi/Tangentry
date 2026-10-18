import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem } from '../../shared/api/types'
import { bilibiliRouteUpdate, bilibiliStateFromURL, type BilibiliArea, type BilibiliTab } from './bilibiliNavigation'

export type { BilibiliArea, BilibiliTab } from './bilibiliNavigation'
export { bilibiliPath, bilibiliRouteUpdate, bilibiliStateFromURL } from './bilibiliNavigation'
export type LiveRoom = { id: string; title: string; author: string; thumbnailUrl: string; url: string }
type Page = { items: ContentItem[]; rooms?: LiveRoom[]; page: number; next?: string; hasMore: boolean; sourceTitle: string; notice?: string }
type Profile = { loggedIn: boolean; id?: string; name?: string; avatar?: string; level?: number }
type State = { page: Page | null; loading: boolean; known: boolean; error: string }
const blank = (): State => ({ page: null, loading: false, known: false, error: '' })
function initialTab(): BilibiliTab {
  return bilibiliStateFromURL().tab
}

function initialArea(): BilibiliArea {
  return bilibiliStateFromURL().area
}

export function useBilibiliBrowse(active: boolean, onRouteChange: (path: string) => void) {
  const routeChangeRef = useRef(onRouteChange)
  routeChangeRef.current = onRouteChange
  const [tab, setTab] = useState<BilibiliTab>(initialTab)
  const [area, setAreaState] = useState<BilibiliArea>(initialArea)
  const setArea = (value: string) => setAreaState(bilibiliStateFromURL(`/bilibili?tab=live&area=${encodeURIComponent(value)}`).area)
  const [states, setStates] = useState<Record<string, State>>({})
  const current = useRef(states); current.current = states
  const [profile, setProfile] = useState<Profile | null>(null)
  const profileRef = useRef(profile); profileRef.current = profile
  const [revision, setRevision] = useState(0)
  const key = tab === 'live' ? `live:${area}` : tab
  const controllers = useRef<Record<string, AbortController>>({})
  const read = useCallback(async (selected: BilibiliTab, roomArea: string, append = false) => {
    const identity = selected === 'live' ? `live:${roomArea}` : selected
    if (controllers.current[identity]) return
    const prior = current.current[identity]?.page ?? null
    if (append && !prior?.hasMore) return
    const controller = new AbortController(); controllers.current[identity] = controller
    setStates(previous => ({ ...previous, [identity]: { ...(previous[identity] ?? blank()), loading: true, error: '' } }))
    try {
      if ((selected === 'my' || selected === 'dynamic') && (!append || !profileRef.current)) {
        const account = await request<Profile>('/bilibili/profile', { signal: controller.signal })
        if (controller.signal.aborted) return
        setProfile(account); profileRef.current = account
        if (!account.loggedIn) {
          setStates(previous => ({ ...previous, [identity]: { page: prior, loading: false, known: true, error: '' } }))
          return
        }
      }
      const page = append ? (prior?.page ?? 0) + 1 : 1
      const query = new URLSearchParams({ page: String(page), ...(selected === 'live' ? { area: roomArea } : {}), ...(append && prior?.next ? { cursor: prior.next } : {}) })
      const result = await request<Page>(`/bilibili/browse/${selected === 'my' ? 'watchlater' : selected}?${query}`, { signal: controller.signal })
      if (controller.signal.aborted) return
      if (append && prior) {
        const ids = new Set(prior.items.map(item => item.id))
        const roomIDs = new Set(prior.rooms?.map(room => room.id))
        const additions = result.items.filter(item => !ids.has(item.id))
        const rooms = result.rooms?.filter(room => !roomIDs.has(room.id)) ?? []
        result.items = [...prior.items, ...additions]
        if (result.rooms) result.rooms = [...(prior.rooms ?? []), ...rooms]
        if (!additions.length && !rooms.length) result.hasMore = false
      }
      setStates(previous => ({ ...previous, [identity]: { page: result, loading: false, known: true, error: '' } }))
    } catch (cause) {
      if (!controller.signal.aborted) setStates(previous => ({ ...previous, [identity]: { ...(previous[identity] ?? blank()), loading: false, known: true, error: cause instanceof Error ? cause.message : '内容暂时无法读取。' } }))
    } finally {
      if (controllers.current[identity] === controller) {
        delete controllers.current[identity]
        if (controller.signal.aborted) setStates(previous => ({ ...previous, [identity]: { ...(previous[identity] ?? blank()), loading: false } }))
      }
    }
  }, [])
  useEffect(() => {
    if (active && !current.current[key]?.known) void read(tab, area)
    if (active) {
      const update = bilibiliRouteUpdate(tab, area)
      routeChangeRef.current(update.path)
    }
    return () => {
      const identities = Object.keys(controllers.current)
      for (const controller of Object.values(controllers.current)) controller.abort()
      controllers.current = {}
      setStates(previous => Object.fromEntries(Object.entries(previous).map(([id, state]) => [id, identities.includes(id) ? { ...state, loading: false } : state])))
    }
  }, [active, tab, area, key, read, revision])
  useEffect(() => {
    const pop = () => {
      if (location.pathname !== '/bilibili') return
      const state = bilibiliStateFromURL()
      setTab(state.tab)
      setArea(state.area)
    }
    window.addEventListener('popstate', pop)
    return () => window.removeEventListener('popstate', pop)
  }, [])
  const invalidate = () => {
    for (const controller of Object.values(controllers.current)) controller.abort()
    controllers.current = {}
    setProfile(null); profileRef.current = null
    const next = Object.fromEntries(Object.entries(current.current).map(([id, state]) => [id, { ...state, loading: false, known: false, error: '' }]))
    current.current = next
    setStates(next)
    setRevision(value => value + 1)
  }
  return { tab, setTab, area, setArea, state: states[key] ?? blank(), profile, refresh: () => void read(tab, area), more: () => void read(tab, area, true), invalidate }
}
