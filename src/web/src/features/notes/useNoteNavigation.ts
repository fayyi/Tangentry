import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { ReadingNote } from '../../shared/api/types'
import { formatPosition } from './noteTime'
import { revealNote } from './noteDom'

interface NoteNavigationOptions {
  contentId: string
  linkedNoteId: string
  notes: ReadingNote[]
  loading: boolean
  readError: string
  visibleNotes: ReadingNote[]
  activeNoteIds: string[]
  resetFilters: () => void
  onNotice: (message: string) => void
  onJumpText?: (id: string, fromLink?: boolean) => void
  onJumpPDF?: (id: string) => void
  onSeek?: (position: number) => boolean | void
  onPreparePlayback?: () => boolean
}

/** Coordinates note links and focus without owning note data or editor state. */
export function useNoteNavigation({
  contentId,
  linkedNoteId,
  notes,
  loading,
  readError,
  visibleNotes,
  activeNoteIds,
  resetFilters,
  onNotice,
  onJumpText,
  onJumpPDF,
  onSeek,
  onPreparePlayback,
}: NoteNavigationOptions) {
  const linkedRequest = useRef({ id: '', found: false, handled: false, prepared: false })
  const linkedFocus = useRef('')
  const focusedNotes = useRef<string[] | null>(null)
  const [linkedHighlight, setLinkedHighlight] = useState('')

  useLayoutEffect(() => {
    const id = linkedFocus.current
    const node = id && id === linkedNoteId ? document.getElementById('reading-note-' + id) : null
    if (!node) return
    linkedFocus.current = ''
    node.focus({ preventScroll: true })
    revealNote(node)
  }, [linkedNoteId, linkedHighlight, visibleNotes])

  useEffect(() => {
    if (linkedRequest.current.id !== linkedNoteId) {
      linkedRequest.current = { id: linkedNoteId, found: false, handled: false, prepared: false }
      linkedFocus.current = ''
      setLinkedHighlight('')
      onNotice('')
    }
    const request = linkedRequest.current
    if (!linkedNoteId || request.handled || loading || readError) return
    const note = notes.find(value => value.id === linkedNoteId && value.contentId === contentId)
    if (!note) {
      request.handled = true
      onNotice('这条笔记已删除或不属于当前内容，其他笔记仍可查看。')
      return
    }
    if (!request.found) {
      request.found = true
      resetFilters()
      linkedFocus.current = note.id
      setLinkedHighlight(note.id)
      if (note.positionSeconds !== null) onNotice(`已找到笔记，播放器就绪后定位到 ${formatPosition(note.positionSeconds)}。`)
    }
    if (note.textAnchor) {
      if (!onJumpText) return
      request.handled = true
      onJumpText(note.id, true)
    } else if (note.pdfAnchor) {
      if (!onJumpPDF) return
      request.handled = true
      onJumpPDF(note.id)
    } else if (note.positionSeconds !== null) {
      if (!onSeek) {
        if (!request.prepared && onPreparePlayback) request.prepared = onPreparePlayback()
        return
      }
      request.handled = true
      if (onSeek(note.positionSeconds) === false) onNotice('已找到笔记，关联时间超出当前媒体范围，暂无法跳转。')
      else onNotice('')
    } else request.handled = true
  }, [contentId, linkedNoteId, loading, readError, notes, onJumpText, onJumpPDF, onSeek, onPreparePlayback, onNotice, resetFilters])

  useEffect(() => {
    if (activeNoteIds.length) resetFilters()
  }, [activeNoteIds, resetFilters])

  useEffect(() => {
    const first = activeNoteIds[0]
    if (!first || focusedNotes.current === activeNoteIds) return
    const node = document.getElementById('reading-note-' + first)
    if (!node) return
    focusedNotes.current = activeNoteIds
    node.focus({ preventScroll: true })
    revealNote(node)
  }, [activeNoteIds, visibleNotes])

  return { linkedHighlight }
}
