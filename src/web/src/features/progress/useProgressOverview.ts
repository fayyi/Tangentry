import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ReadingProgress } from '../../shared/api/types'
import { subscribeProgressSaved } from './changes'

export type ProgressFilter = 'all' | 'not-started' | 'in-progress' | 'completed'
export const supportsProgress = (id: string) => /^(bilibili:BV[0-9A-Za-z]{10}|youtube:[A-Za-z0-9_-]{11}|xiaohongshu:[a-f0-9]{24}|xiaoyuzhou:[a-f0-9]{24}|douyin:[1-9][0-9]{15,19}|rss:[a-f0-9]{64})$/.test(id)
export function progressMatches(progress: ReadingProgress | null, filter: ProgressFilter) {
  if (filter === 'all') return true
  if (filter === 'completed') return Boolean(progress?.completed)
  if (filter === 'in-progress') return Boolean(progress && !progress.completed && progress.positionSeconds > 0)
  return !progress || (!progress.completed && progress.positionSeconds === 0)
}

export function useProgressOverview(contentIds: string[], active: boolean) {
  const key = JSON.stringify([...new Set(contentIds.filter(supportsProgress))].sort())
  const ids = useMemo(() => JSON.parse(key) as string[], [key])
  const [snapshot, setSnapshot] = useState<{ key: string; values: Map<string, ReadingProgress> } | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<ProgressFilter>('all')
  const reader = useRef<AbortController | null>(null)
  const loadedKey = useRef('')

  const reload = useCallback(async () => {
    if (!active) return
    reader.current?.abort()
    const controller = new AbortController()
    reader.current = controller
    if (loadedKey.current !== key) setLoading(true)
    try {
      const values = new Map<string, ReadingProgress>()
      for (let offset = 0; offset < ids.length; offset += 100) {
        const query = new URLSearchParams()
        for (const id of ids.slice(offset, offset + 100)) query.append('contentId', id)
        const result = await request<{ items: ReadingProgress[] }>(`/reading-progress/batch?${query}`, { signal: controller.signal })
        if (controller.signal.aborted) return
        for (const progress of result.items) values.set(progress.contentId, progress)
      }
      if (!controller.signal.aborted) { setSnapshot({ key, values }); setError(''); loadedKey.current = key }
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '进度暂时无法读取。')
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [active, ids, key])

  useEffect(() => {
    if (!active) return
    void reload()
    const refresh = () => { if (!document.hidden) void reload() }
    const unsubscribe = subscribeProgressSaved(id => { if (ids.includes(id)) void reload() })
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      reader.current?.abort()
      unsubscribe()
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [active, ids, reload])

  const available = snapshot?.key === key && !error
  const effectiveFilter = available ? filter : 'all'
  const get = (id: string): ReadingProgress | null | undefined => available && ids.includes(id) && supportsProgress(id) ? snapshot.values.get(id) ?? null : undefined
  return { get, loading, error, available, filter, effectiveFilter, setFilter, reload,
    matches: (id: string) => effectiveFilter === 'all' || (supportsProgress(id) && progressMatches(get(id) ?? null, effectiveFilter)) }
}
