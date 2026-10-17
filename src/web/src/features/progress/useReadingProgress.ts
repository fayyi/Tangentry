import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ReadingProgress } from '../../shared/api/types'
import { notifyProgressSaved } from './changes'

interface Sample { positionSeconds: number; durationSeconds: number }
const same = (a: Sample | null, b: Sample | null) => Boolean(a && b && a.positionSeconds === b.positionSeconds && a.durationSeconds === b.durationSeconds)
const explain = (cause: unknown) => cause instanceof Error ? cause.message : '阅读进度没有保存，请重试。'

export function useReadingProgress(contentId: string) {
  const [saved, setSaved] = useState<ReadingProgress | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [readError, setReadError] = useState('')
  const [writeError, setWriteError] = useState('')
  const alive = useRef(true)
  const loaded = useRef(false)
  const blocked = useRef(false)
  const inFlight = useRef(false)
  const revision = useRef(0)
  const confirmed = useRef<ReadingProgress | null>(null)
  const sampled = useRef<Sample | null>(null)
  const pending = useRef<Sample | null>(null)
  const urgent = useRef(false)
  const timer = useRef<number | null>(null)
  const reader = useRef<AbortController | null>(null)
  const pumpRef = useRef<() => void>(() => {})

  function schedule(immediate: boolean) {
    urgent.current ||= immediate
    if (immediate && timer.current !== null) { clearTimeout(timer.current); timer.current = null }
    if (!loaded.current || blocked.current || inFlight.current || !pending.current) return
    if (urgent.current || !alive.current) { pumpRef.current(); return }
    // Keep the resume point close to the visible player clock without writing
    // on every `timeupdate` event. A short debounce also coalesces rapid seeks.
    if (timer.current === null) timer.current = window.setTimeout(() => { timer.current = null; pumpRef.current() }, 1500)
  }

  const pump = useCallback(async () => {
    if (!loaded.current || blocked.current || inFlight.current || !pending.current) return
    const sample = pending.current
    pending.current = null
    urgent.current = false
    if (same(sample, confirmed.current)) return
    inFlight.current = true
    if (alive.current) setSaving(true)
    try {
      const value = await request<ReadingProgress>(`/reading-progress/${encodeURIComponent(contentId)}`, {
        method: 'PUT', body: JSON.stringify({ ...sample, revision: revision.current }), keepalive: true,
      })
      confirmed.current = value
      revision.current = value.revision
      notifyProgressSaved(contentId)
      if (alive.current) { setSaved(value); setWriteError('') }
    } catch (cause) {
      pending.current ??= sample
      blocked.current = true // Never silently overwrite a newer page after a 409.
      if (alive.current) setWriteError(explain(cause))
    } finally {
      inFlight.current = false
      if (alive.current) setSaving(false)
      schedule(urgent.current)
    }
  }, [contentId])
  pumpRef.current = () => { void pump() }

  const observe = useCallback((positionSeconds: number, durationSeconds: number, immediate = false) => {
    if (!Number.isFinite(positionSeconds) || !Number.isFinite(durationSeconds) || durationSeconds <= 0 || durationSeconds > 604800 || positionSeconds < 0 || positionSeconds > durationSeconds) return
    const sample = { positionSeconds: Math.round(positionSeconds * 1000) / 1000, durationSeconds: Math.round(durationSeconds * 1000) / 1000 }
    if (sample.durationSeconds <= 0) return
    sampled.current = sample
    if (same(sample, confirmed.current) && !inFlight.current) { pending.current = null; return }
    pending.current = sample
    schedule(immediate)
  }, [])

  const reload = useCallback(async (): Promise<boolean> => {
    if (inFlight.current) return false
    reader.current?.abort()
    const controller = new AbortController()
    reader.current = controller
    loaded.current = false
    if (alive.current) { setLoading(true); setReadError('') }
    try {
      const result = await request<{ progress: ReadingProgress | null }>(`/reading-progress?contentId=${encodeURIComponent(contentId)}`, { signal: controller.signal })
      if (controller.signal.aborted) return false
      confirmed.current = result.progress
      revision.current = result.progress?.revision ?? 0
      loaded.current = true
      if (alive.current) setSaved(result.progress)
      schedule(urgent.current)
      return true
    } catch (cause) {
      if (alive.current && !controller.signal.aborted) setReadError(explain(cause))
      return false
    } finally {
      if (alive.current && !controller.signal.aborted) setLoading(false)
    }
  }, [contentId])

  const retry = useCallback(async () => {
    // An unsuccessful response might already have committed. Explicitly re-read
    // the revision before the user chooses to persist this page's current position.
    if (!(await reload())) return
    blocked.current = false
    if (alive.current) setWriteError('')
    if (sampled.current) { pending.current = sampled.current; schedule(true) }
  }, [reload])

  useEffect(() => {
    alive.current = true
    void reload()
    const flush = () => { if (sampled.current) observe(sampled.current.positionSeconds, sampled.current.durationSeconds, true) }
    const hidden = () => { if (document.hidden) flush() }
    window.addEventListener('pagehide', flush)
    document.addEventListener('visibilitychange', hidden)
    return () => {
      alive.current = false
      reader.current?.abort()
      if (timer.current !== null) { clearTimeout(timer.current); timer.current = null }
      flush()
      window.removeEventListener('pagehide', flush)
      document.removeEventListener('visibilitychange', hidden)
    }
  }, [reload, observe])

  return { saved, loading, saving, readError, writeError, observe, reload, retry }
}
