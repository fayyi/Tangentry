import { useEffect, useRef, useState, type RefObject } from 'react'
import { originalRunAt, originalRuns, readingRun } from './articleViewport'

const storageKey = 'tangentry.article-position.v1'
interface Position { contentId: string; sourceHash: string; offset: number; updatedAt: number }

function positions(): Position[] {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw || raw.length > 100_000) return []
    const values: unknown = JSON.parse(raw)
    if (!Array.isArray(values)) return []
    return values.filter((value): value is Position => value && typeof value.contentId === 'string' && value.contentId.length <= 200 &&
      typeof value.sourceHash === 'string' && /^[a-f0-9]{64}$/.test(value.sourceHash) &&
      Number.isInteger(value.offset) && value.offset > 0 && value.offset <= 2_000_000 &&
      Number.isSafeInteger(value.updatedAt) && value.updatedAt >= 0).slice(0, 100)
  } catch { return [] }
}

function persist(contentId: string, sourceHash: string, offset: number) {
  try {
    const stored = positions()
    const existing = stored.find(value => value.contentId === contentId && value.sourceHash === sourceHash)
    if (existing?.offset === offset || offset === 0 && !existing) return
    const previous = stored.filter(value => value.contentId !== contentId || value.sourceHash !== sourceHash)
    const next = offset > 0 ? [{ contentId, sourceHash, offset, updatedAt: Date.now() }, ...previous] : previous
    localStorage.setItem(storageKey, JSON.stringify(next.slice(0, 100)))
  } catch { /* Reading remains usable when optional browser storage is unavailable. */ }
}

export function useArticlePosition(root: RefObject<HTMLElement | null>, contentId: string, sourceHash: string, textLength: number, focusToken?: number) {
  const identity = contentId + ':' + sourceHash
  const [saved, setSaved] = useState<{ identity: string; offset: number | null }>({ identity: '', offset: null })
  const [restored, setRestored] = useState('')
  const cancelSample = useRef<() => void>(() => {})
  const lastView = useRef<{ identity: string; run: ReturnType<typeof readingRun> } | null>(null)

  useEffect(() => {
    if (!contentId || !sourceHash) return
    const value = positions().find(value => value.contentId === contentId && value.sourceHash === sourceHash && value.offset < textLength)
    setSaved({ identity, offset: value?.offset ?? null })
    setRestored('')
    lastView.current = { identity, run: root.current ? readingRun(originalRuns(root.current)) : null }
  }, [identity, contentId, sourceHash, textLength, root])

  useEffect(() => {
    if (!contentId || !sourceHash) return
    let intentional = false
    let pending: number | null = null
    let frame: number | null = null
    let timer: number | null = null
    let nodes: HTMLElement[] | null = null
    const observer = new MutationObserver(() => { nodes = null; flush(); reset() })
    if (root.current) observer.observe(root.current, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['data-article-start', 'data-article-end'] })
    const flush = () => {
      if (timer !== null) { clearTimeout(timer); timer = null }
      if (pending !== null) { persist(contentId, sourceHash, pending); pending = null }
    }
    const reset = () => {
      intentional = false
      pending = null
      if (frame !== null) { cancelAnimationFrame(frame); frame = null }
      if (timer !== null) { clearTimeout(timer); timer = null }
    }
    cancelSample.current = () => { flush(); reset() }
    const intent = (event: Event) => {
      const target = event.target instanceof Element ? event.target : null
      if (event.defaultPrevented || target?.closest('.reading-notes, input, textarea, select, [contenteditable=true]')) return
      if (event instanceof WheelEvent && event.ctrlKey) return
      if (event instanceof KeyboardEvent) {
        if (event.altKey || event.ctrlKey || event.metaKey || !['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End', ' '].includes(event.key)) return
        if (target?.closest('a, button, summary, [role=button]')) return
      }
      intentional = true
    }
    const sample = (includeHidden = false) => {
      frame = null
      if (!intentional || !includeHidden && document.hidden || !root.current) return
      // Record original paragraph coordinates, independent of translated text height.
      nodes ??= originalRuns(root.current)
      const run = readingRun(nodes)
      if (!run || run.offset >= textLength) return
      lastView.current = { identity, run }
      pending = run.offset
      if (timer === null) timer = window.setTimeout(flush, 1000)
    }
    const scroll = () => { if (intentional && frame === null) frame = requestAnimationFrame(() => sample()) }
    const depart = () => {
      if (frame !== null) cancelAnimationFrame(frame)
      sample(true)
      flush()
    }
    const hidden = () => { if (document.hidden) depart() }
    window.addEventListener('wheel', intent, { passive: true })
    window.addEventListener('touchmove', intent, { passive: true })
    window.addEventListener('keydown', intent)
    window.addEventListener('scroll', scroll, { passive: true })
    window.addEventListener('pagehide', depart)
    document.addEventListener('visibilitychange', hidden)
    return () => {
      flush()
      reset()
      observer.disconnect()
      window.removeEventListener('wheel', intent)
      window.removeEventListener('touchmove', intent)
      window.removeEventListener('keydown', intent)
      window.removeEventListener('scroll', scroll)
      window.removeEventListener('pagehide', depart)
      document.removeEventListener('visibilitychange', hidden)
    }
  }, [root, contentId, sourceHash, textLength, focusToken, identity])

  const offset = saved.identity === identity && restored !== identity ? saved.offset : null
  function restore() {
    if (offset === null || !root.current) return
    const node = originalRunAt(root.current, offset)
    if (!node) return
    cancelSample.current()
    root.current.focus({ preventScroll: true })
    node.scrollIntoView({ block: 'start', behavior: 'instant' })
    lastView.current = { identity, run: readingRun(originalRuns(root.current)) }
    persist(contentId, sourceHash, offset)
    setRestored(identity)
  }
  const capture = () => root.current && (readingRun(originalRuns(root.current)) ?? (lastView.current?.identity === identity ? lastView.current.run : null))
  return { available: offset !== null && focusToken === undefined, restore, capture, pause: () => cancelSample.current() }
}
