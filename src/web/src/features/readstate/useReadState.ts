import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ReadState } from '../../shared/api/types'

const identity = /^(bilibili:BV[0-9A-Za-z]{10}|youtube:[A-Za-z0-9_-]{11}|douyin:[1-9][0-9]{15,19}|xiaohongshu:[a-f0-9]{24}|rss:[a-f0-9]{64}|pdf:[a-f0-9]{64}|xiaoyuzhou:[a-f0-9]{24})$/
export function supportsReadState(id: string) { return identity.test(id) }

export function useReadState(ids: string[]) {
  const key = JSON.stringify([...new Set(ids.filter(supportsReadState))].sort())
  const [states, setStates] = useState<Map<string, ReadState>>(new Map())
  const [resolvedKey, setResolvedKey] = useState('')
  const [loading, setLoading] = useState(false), [loadError, setLoadError] = useState(''), [saveError, setSaveError] = useState('')
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [version, setVersion] = useState(0)
  const reading = useRef<AbortController | null>(null), generation = useRef(0), saving = useRef(false)
  const mounted = useRef(true)
  const resolved = useRef('')
  const refresh = useCallback(() => { if (!saving.current) { setSaveError(''); setVersion(current => current + 1) } }, [])

  useEffect(() => {
    const requested = JSON.parse(key) as string[]
    reading.current?.abort()
    const controller = new AbortController(), token = ++generation.current
    reading.current = controller
    setLoadError('')
    if (!requested.length) { setLoading(false); setStates(new Map()); setResolvedKey(key); return }
    if (resolved.current !== key) setLoading(true)
    const query = new URLSearchParams(requested.map(id => ['contentId', id]))
    void request<{ states: ReadState[] }>(`/read-state?${query}`, { signal: controller.signal }).then(result => {
      if (controller.signal.aborted || token !== generation.current) return
      setStates(new Map(result.states.map(state => [state.contentId, state])))
      setResolvedKey(key)
      resolved.current = key
    }).catch(cause => {
      if (!controller.signal.aborted && token === generation.current) setLoadError(cause instanceof Error ? cause.message : '暂时无法读取已读状态。')
    }).finally(() => {
      if (!controller.signal.aborted && token === generation.current) setLoading(false)
    })
    return () => controller.abort()
  }, [key, version])

  useEffect(() => {
    mounted.current = true
    const visible = () => { if (document.visibilityState === 'visible') refresh() }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', visible)
    return () => { mounted.current = false; reading.current?.abort(); window.removeEventListener('focus', refresh); document.removeEventListener('visibilitychange', visible) }
  }, [refresh])

  const setRead = useCallback(async (contentIds: string[], read: boolean) => {
    if (saving.current || !contentIds.length) return false
    const selected = [...new Set(contentIds)]
    saving.current = true
    reading.current?.abort(); ++generation.current
    setLoading(false); setPending(new Set(selected)); setSaveError('')
    try {
      const result = await request<{ states: ReadState[] }>('/read-state', { method: 'PUT', body: JSON.stringify({ contentIds: selected, read }) })
      // A batch read issued during this write must not restore its old value.
      reading.current?.abort(); ++generation.current
      if (mounted.current) setStates(current => { const next = new Map(current); for (const state of result.states) next.set(state.contentId, state); return next })
      return true
    } catch (cause) {
      if (mounted.current) setSaveError(cause instanceof Error ? cause.message : '已读状态未保存，请重试。')
      return false
    } finally {
      saving.current = false
      if (mounted.current) { setPending(new Set()); setVersion(current => current + 1) }
    }
  }, [])

  return { states, loading, error: saveError || loadError, pending, refresh, setRead, available: resolvedKey === key && !loadError, ready: resolvedKey === key && !loading && !loadError, get: (id: string) => states.get(id) }
}
