import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import { APIError } from '../../shared/api/client'
import type { ReadingNote, TextAnchor, PDFAnchor, SubtitleReference, SummaryAnchor } from '../../shared/api/types'
import { notifyNotesChanged, subscribeNotesChanged } from './changes'
import { changeTaskState } from './taskList'

export function useNotes(contentId: string) {
  const [notes, setNotes] = useState<ReadingNote[]>([])
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [readError, setReadError] = useState('')
  const [writeError, setWriteError] = useState('')
  const [writeOperation, setWriteOperation] = useState<'save' | 'delete' | 'task' | 'frame' | null>(null)
  const [writeErrorCode, setWriteErrorCode] = useState('')
  const listRequest = useRef<AbortController | null>(null)
  const mutation = useRef<AbortController | null>(null)
  const generation = useRef(0)
  const refreshAfterWrite = useRef(false)
  const publishing = useRef(false)
  const loadedFor = useRef<string | null>(null)

  const reload = useCallback(async (): Promise<ReadingNote[] | null> => {
    if (mutation.current) { refreshAfterWrite.current = true; return null }
    refreshAfterWrite.current = false
    listRequest.current?.abort()
    const controller = new AbortController()
    const token = ++generation.current
    listRequest.current = controller
    if (loadedFor.current !== contentId) setLoading(true)
    setReadError('')
    try {
      const result = await request<{ notes: ReadingNote[] }>(`/notes?contentId=${encodeURIComponent(contentId)}`, { signal: controller.signal })
      if (!controller.signal.aborted && token === generation.current) { setNotes(result.notes); loadedFor.current = contentId; return result.notes }
    } catch (cause) {
      if (!controller.signal.aborted && token === generation.current) setReadError(cause instanceof Error ? cause.message : '暂时无法读取笔记。')
      return null
    } finally {
      if (!controller.signal.aborted && token === generation.current) { setLoading(false); listRequest.current = null }
    }
    return null
  }, [contentId])

  useEffect(() => {
    void reload()
    const focus = () => { void reload() }
    const visible = () => { if (document.visibilityState === 'visible') void reload() }
    const unsubscribe = subscribeNotesChanged(id => { if (id === contentId && !publishing.current) void reload() })
    window.addEventListener('focus', focus)
    document.addEventListener('visibilitychange', visible)
    return () => {
      ++generation.current
      listRequest.current?.abort()
      mutation.current?.abort()
      unsubscribe()
      window.removeEventListener('focus', focus)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [reload, contentId])

  function announceChange() {
    publishing.current = true
    try { notifyNotesChanged(contentId) }
    finally { publishing.current = false }
  }

  async function save(body: string, positionSeconds: number | null, editingId: string | null, textAnchor?: TextAnchor | null, pdfAnchor?: PDFAnchor | null, tags?: string[], expectedUpdatedAt?: number, operation: 'save' | 'task' = 'save', subtitleReferences?: SubtitleReference[], creation?: { createId: string; frame?: { downloadId: string; positionSeconds: number }; image?: { data: string; positionSeconds: number }; images?: { data: string; positionSeconds: number }[] }, summaryAnchor?: SummaryAnchor | null): Promise<ReadingNote | null> {
    if (mutation.current || listRequest.current || loading || readError) return null
    const controller = new AbortController()
    mutation.current = controller
    setPending(true)
    setWriteOperation(operation)
    setWriteError('')
    setWriteErrorCode('')
    try {
      const input = { contentId, body, positionSeconds, textAnchor, pdfAnchor, summaryAnchor, tags, subtitleReferences, createId: creation?.createId }
      const note = await request<ReadingNote>(editingId ? `/notes/${encodeURIComponent(editingId)}` : creation?.images?.length ? '/notes/with-image' : creation?.image ? '/notes/with-image' : creation?.frame ? '/notes/with-frame' : '/notes', {
        method: editingId ? 'PATCH' : 'POST',
        body: JSON.stringify(editingId ? { body, positionSeconds, textAnchor, pdfAnchor, summaryAnchor, tags, expectedUpdatedAt, subtitleReferences } : creation?.images?.length ? { note: input, images: creation.images } : creation?.image ? { note: input, image: creation.image } : creation?.frame ? { note: input, capture: creation.frame } : input),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return null
      setNotes(current => editingId ? current.map(item => item.id === note.id ? note : item) : [note, ...current.filter(item => item.id !== note.id)])
      announceChange()
      return note
    } catch (cause) {
      if (!controller.signal.aborted) { setWriteError(cause instanceof Error ? cause.message : '笔记没有保存，请重试。'); setWriteErrorCode(cause instanceof APIError ? cause.code : ''); refreshAfterWrite.current = true }
      return null
    } finally {
      if (mutation.current === controller) {
        mutation.current = null
        if (!controller.signal.aborted) {
          setPending(false)
          if (refreshAfterWrite.current) void reload()
        }
      }
    }
  }

  async function remove(id: string, expectedUpdatedAt: number): Promise<boolean> {
    if (mutation.current || listRequest.current || loading || readError) return false
    const controller = new AbortController()
    mutation.current = controller
    setPending(true)
    setWriteOperation('delete')
    setWriteError('')
    setWriteErrorCode('')
    try {
      await request<void>(`/notes/${encodeURIComponent(id)}?expectedUpdatedAt=${expectedUpdatedAt}`, { method: 'DELETE', signal: controller.signal })
      if (controller.signal.aborted) return false
      setNotes(current => current.filter(note => note.id !== id))
      announceChange()
      return true
    } catch (cause) {
      if (!controller.signal.aborted) { setWriteError(cause instanceof Error ? cause.message : '笔记没有删除，请重试。'); setWriteErrorCode(cause instanceof APIError ? cause.code : '') }
      return false
    } finally {
      if (mutation.current === controller) {
        mutation.current = null
        if (!controller.signal.aborted) {
          setPending(false)
          if (refreshAfterWrite.current) void reload()
        }
      }
    }
  }

  async function toggleTask(note: ReadingNote, offset: number, checked: boolean) {
    const body = changeTaskState(note.body, offset, checked)
    if (body === null || body === note.body) return null
    return save(body, note.positionSeconds, note.id, undefined, undefined, undefined, note.updatedAt, 'task')
  }

  async function frame(note: ReadingNote, capture?: { downloadId: string; positionSeconds: number }, image?: { data: string; positionSeconds: number }, frameId?: string) {
    if (mutation.current || listRequest.current || loading || readError) return null
    const controller = new AbortController()
    mutation.current = controller
    setPending(true); setWriteOperation('frame'); setWriteError(''); setWriteErrorCode('')
    try {
      const path = frameId ? `/notes/${note.id}/frames/${encodeURIComponent(frameId)}?expectedUpdatedAt=${note.updatedAt}` : image ? `/notes/${note.id}/frames/image` : `/notes/${note.id}/frames`
      const saved = await request<ReadingNote>(path, {
        method: frameId ? 'DELETE' : 'POST', signal: controller.signal,
        body: frameId ? undefined : JSON.stringify({ ...(image ?? capture), expectedUpdatedAt: note.updatedAt }),
      })
      if (controller.signal.aborted) return null
      setNotes(current => current.map(value => value.id === saved.id ? saved : value))
      announceChange()
      return saved
    } catch (cause) {
      if (!controller.signal.aborted) {
        setWriteError(cause instanceof Error ? cause.message : '画面没有保存，请重试。')
        // Frame conflicts refresh their own record; they must not open the
        // body editor's replace-version flow for a different writing draft.
        setWriteErrorCode(cause instanceof APIError && cause.code !== 'note_conflict' ? cause.code : '')
        // A response may be lost after committing; refresh before a retry.
        refreshAfterWrite.current = true
      }
      return null
    } finally {
      if (mutation.current === controller) {
        mutation.current = null
        if (!controller.signal.aborted) {
          setPending(false)
          if (refreshAfterWrite.current) void reload()
        }
      }
    }
  }

  function cancelFrame() {
    if (writeOperation !== 'frame' || !mutation.current) return
    mutation.current.abort()
    mutation.current = null
    setPending(false)
    void reload()
  }

  return { notes, loading, pending, readError, writeError, writeOperation, writeErrorCode, reload, save, remove, toggleTask, frame, cancelFrame }
}
