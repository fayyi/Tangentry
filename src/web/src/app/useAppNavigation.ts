import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { popped, pushPath } from '../shared/browser/historyStack'
import { readerNoteFromURL, readerQueryFromURL, viewFromURL, viewPaths, type View } from './navigation'

export type ReaderReturnView = Exclude<View, 'reader' | 'tools' | 'search'> | 'search' | 'tools'

export function selectedSavedViewFromURL() {
  return viewFromURL() === 'subscriptions' ? new URLSearchParams(location.search).get('view') ?? '' : ''
}

type ViewportTarget = { top: number; sourceID: string }

const initialScrollPositions = (): Record<View, number> => ({
  bilibili: 0,
  history: 0,
  discovery: 0,
  podcasts: 0,
  search: 0,
  library: 0,
  downloads: 0,
  tools: 0,
  reader: 0,
  subscriptions: 0,
  notes: 0,
})

export function RestoreViewport({ top, sourceID, view }: ViewportTarget & { view: View }) {
  useLayoutEffect(() => {
    window.scrollTo({ top, behavior: 'instant' })
    if (document.documentElement.dataset.input === 'keyboard') document.getElementById('main')?.focus({ preventScroll: true })
    if (sourceID) {
      const candidates = Array.from(document.querySelectorAll<HTMLElement>(`[data-content-id="${CSS.escape(sourceID)}"]`))
      const source = candidates.find(node => view === 'discovery'
        ? node.closest('[data-app-view="discovery"]')
        : !node.closest('[data-app-view="discovery"]'))
      source?.focus({ preventScroll: true })
    }
  }, [sourceID, top, view])
  return null
}

export function useAppNavigation(input: RefObject<HTMLInputElement | null>) {
  const [view, setView] = useState<View>(viewFromURL)
  const [readerQuery, setReaderQuery] = useState(readerQueryFromURL)
  const [readerNoteId, setReaderNoteId] = useState(readerNoteFromURL)
  const [readingItem, setReadingItem] = useState<import('../shared/api/types').ContentItem | null>(null)
  const readerReturn = useRef<ReaderReturnView>(readerNoteId ? 'notes' : 'discovery')
  const readerSourceID = useRef(readerNoteId ? `note:${readerNoteId}` : '')
  const scrollPositions = useRef<Record<View, number>>(initialScrollPositions())
  const currentView = useRef(view)
  const viewportTarget = useRef<ViewportTarget>({ top: 0, sourceID: '' })

  function navigate(next: View, push = true, path = viewPaths[next]) {
    const returningFromReader = currentView.current === 'reader' && next === readerReturn.current
    scrollPositions.current[currentView.current] = window.scrollY
    currentView.current = next
    viewportTarget.current = { top: scrollPositions.current[next], sourceID: returningFromReader ? readerSourceID.current : '' }
    if (push) pushPath(path)

    setReaderQuery(readerQueryFromURL())
    const noteId = readerNoteFromURL()
    setReaderNoteId(noteId)
    if (next === 'reader' && noteId) {
      readerReturn.current = 'notes'
      readerSourceID.current = `note:${noteId}`
    }
    setView(next)
  }

  function returnHome(animate: boolean) {
    if (currentView.current !== 'discovery') navigate('discovery')
    scrollPositions.current.discovery = 0
    requestAnimationFrame(() => window.scrollTo({ top: 0, behavior: animate ? 'smooth' : 'instant' }))
  }

  useEffect(() => {
    const popstate = (event: PopStateEvent) => {
      popped(event.state)
      navigate(viewFromURL(), false)
    }
    const cmdK = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === 'k') {
        event.preventDefault()
        if (view !== 'search') navigate('search')
        window.setTimeout(() => (document.querySelector<HTMLInputElement>('.unified-search-form input') ?? input.current)?.focus(), 100)
      }
    }
    window.addEventListener('popstate', popstate)
    window.addEventListener('keydown', cmdK)
    return () => {
      window.removeEventListener('popstate', popstate)
      window.removeEventListener('keydown', cmdK)
    }
  }, [view])

  return {
    view,
    navigate,
    returnHome,
    readerQuery,
    readerNoteId,
    readingItem,
    setReadingItem,
    readerReturn,
    readerSourceID,
    scrollPositions,
    viewportTarget,
  }
}
