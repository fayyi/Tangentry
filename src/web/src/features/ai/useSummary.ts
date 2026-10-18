import { useEffect, useMemo, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SubtitleTrack } from '../../shared/api/types'
import type { SummaryInspection, SummaryResult } from './summaryTypes'
import { usePageVisibility } from '../../shared/hooks/usePageVisibility'
import { notifyNotesChanged } from '../notes/changes'

export interface SummarySegmentInput { index: number; start?: number; end?: number; page?: number; text: string }

export function summaryInputFromTrack(contentId: string, source: string, track: SubtitleTrack): { contentId: string; source: string; segments: SummarySegmentInput[]; revision: number } {
  return { contentId, source, segments: track.cues.map((cue, index) => ({ index, start: cue.start, end: cue.end, text: cue.text })), revision: 0 }
}

export function useSummary(contentId: string, source: string, track?: SubtitleTrack, onSeek?: (seconds: number) => void, text?: string, segments?: SummarySegmentInput[]) {
  const visible = usePageVisibility()
  const input = useMemo(() => track ? summaryInputFromTrack(contentId, source, track) : segments?.length ? { contentId, source, segments, revision: 0 } : text?.trim() ? { contentId, source, text, revision: 0 } : null, [contentId, source, track, text, segments])
  const scope = useMemo(() => input ? JSON.stringify(input) : '', [input])
  const liveScope = useRef(scope); liveScope.current = scope
  const [inspection, setInspection] = useState<SummaryInspection | null>(null)
  const [dataScope, setDataScope] = useState('')
  const [loading, setLoading] = useState(false), [pending, setPending] = useState(false)
  const [error, setError] = useState(''), [notice, setNotice] = useState('')
  const readController = useRef<AbortController | null>(null), mutation = useRef<AbortController | null>(null)
  const reload = async () => {
    if (!input) return
    readController.current?.abort(); const controller = new AbortController(); readController.current = controller
    const requestedScope = scope; if (dataScope !== requestedScope) setLoading(true)
    try {
      const value = await request<SummaryInspection>('/ai/summaries/inspect', { method: 'POST', signal: controller.signal, body: JSON.stringify(input) })
      if (!controller.signal.aborted && liveScope.current === requestedScope) {
        setInspection(value); setDataScope(requestedScope); setError('')
      }
    } catch (cause) { if (!controller.signal.aborted && liveScope.current === requestedScope) setError(cause instanceof Error ? cause.message : '本机摘要暂不可用。') }
    finally {
      if (readController.current === controller) readController.current = null
      if (!controller.signal.aborted && liveScope.current === requestedScope) setLoading(false)
    }
  }
  useEffect(() => {
    setInspection(null); setDataScope(''); setError(''); setNotice(''); setPending(false); setLoading(false)
    if (input) void reload()
    return () => {
      readController.current?.abort(); readController.current = null
      mutation.current?.abort(); mutation.current = null
    }
  }, [scope])
  const refreshOnReturn = useRef<() => void>(() => {})
  refreshOnReturn.current = () => {
    if (input && !mutation.current && !readController.current) void reload()
  }
  useEffect(() => {
    if (!scope) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const refresh = () => {
      if (timer) clearTimeout(timer)
      if (document.visibilityState === 'hidden') return
      timer = setTimeout(() => {
        if (document.visibilityState !== 'hidden') refreshOnReturn.current()
      }, 80)
    }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      if (timer) clearTimeout(timer)
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [scope])
  useEffect(() => {
    if (!visible || pending || loading || error || dataScope !== scope || (!inspection?.active && !inspection?.settings.translating)) return
    // Each inspection is a new object, so every answer re-arms the next check.
    const timer = setTimeout(() => void reload(), 1000)
    return () => clearTimeout(timer)
  }, [visible, pending, loading, error, dataScope, scope, inspection])
  // regenerate replaces the stored summary with one from the current model.
  async function generate(regenerate = false) {
    if (!input || !inspection || dataScope !== scope || loading || pending || mutation.current || inspection.active || inspection.settings.translating || !(inspection.settings.summaryReady ?? inspection.settings.ready)) return
    const controller = new AbortController(); mutation.current = controller; const requestedScope = scope; const snapshot = inspection
    setPending(true); setError(''); setNotice('')
    if (regenerate) setInspection({ ...snapshot, result: { ...snapshot.result, complete: false, completedCalls: 0, overview: undefined, points: undefined, citations: undefined } })
    let timer: ReturnType<typeof setTimeout> | undefined
    const progress = async () => {
      if (controller.signal.aborted) return
      try { const value = await request<SummaryResult>(`/ai/summaries/${snapshot.result.key}`, { signal: controller.signal }); if (!controller.signal.aborted && liveScope.current === requestedScope) setInspection(previous => previous ? { ...previous, result: value } : previous) } catch { /* the final mutation response is authoritative */ }
      if (!controller.signal.aborted) timer = setTimeout(() => void progress(), 1000)
    }
    timer = setTimeout(() => void progress(), 700)
    try {
      const value = await request<SummaryResult>('/ai/summaries/generate', { method: 'POST', signal: controller.signal, body: JSON.stringify({ ...input, revision: snapshot.settings.revision, ...(regenerate ? { regenerate } : {}) }) })
      if (!controller.signal.aborted && liveScope.current === requestedScope) {
        setInspection({ ...snapshot, result: value, active: false }); setNotice('摘要已保存到本机。')
        notifyNotesChanged(value.contentId)
      }
    } catch (cause) {
      if (!controller.signal.aborted && liveScope.current === requestedScope) {
        try { const value = await request<SummaryResult>(`/ai/summaries/${snapshot.result.key}`, { signal: controller.signal }); if (!controller.signal.aborted && liveScope.current === requestedScope) setInspection({ ...snapshot, result: value, active: false }) } catch { /* keep committed progress */ }
        if (!controller.signal.aborted && liveScope.current === requestedScope) setError(cause instanceof Error ? cause.message : '摘要未完成，已完成部分仍保留。')
      }
    } finally { if (timer) clearTimeout(timer); controller.abort(); if (mutation.current === controller) { mutation.current = null; if (liveScope.current === requestedScope) setPending(false) } }
  }
  function cancel() { mutation.current?.abort(); mutation.current = null; setPending(false); setNotice('已取消摘要，已完成部分保留。'); void reload() }
  return { contentId, input, hasInput: Boolean(input), inspection: dataScope === scope ? inspection : null, loading, pending, error, notice, reload, generate, cancel, onSeek }
}
