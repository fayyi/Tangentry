import { useEffect, useState } from 'react'
import { dateRange } from './timeline'
import { validTag } from './tags'

export interface NoteViewSelection { query: string; tag: string; fromDate: string; toDate: string; pinnedOnly?: boolean }
export interface SavedNoteView extends NoteViewSelection { id: string; name: string }
const storageKey = ['tangentry', 'note-views', 'v1'].join('.')
const changedEvent = 'tangentry-note-views-changed'
const maxViews = 30
class ViewChangeError extends Error {}
export const selectionKey = (value: NoteViewSelection) => JSON.stringify([value.query, value.tag, value.fromDate, value.toDate, Boolean(value.pinnedOnly)])
export const hasSelection = (value: NoteViewSelection) => Boolean(value.query || value.tag || value.fromDate || value.toDate || value.pinnedOnly)

function readViews(): SavedNoteView[] {
  const raw = localStorage.getItem(storageKey)
  if (!raw) return []
  const value: unknown = JSON.parse(raw)
  if (!Array.isArray(value) || value.length > maxViews) throw new Error()
  const ids = new Set<string>()
  return value.map(item => {
    if (!item || typeof item !== 'object' ||
      typeof item.id !== 'string' || !item.id || item.id.length > 64 || ids.has(item.id) ||
      typeof item.name !== 'string' || !item.name.trim() || Array.from(item.name).length > 64 ||
      typeof item.query !== 'string' || Array.from(item.query).length > 200 ||
      typeof item.tag !== 'string' || (item.tag !== '' && !validTag(item.tag)) ||
      typeof item.fromDate !== 'string' || typeof item.toDate !== 'string' ||
      (item.pinnedOnly !== undefined && typeof item.pinnedOnly !== 'boolean') ||
      !dateRange(item.fromDate, item.toDate) || !hasSelection(item)) throw new Error()
    ids.add(item.id)
    return { id: item.id, name: item.name, query: item.query, tag: item.tag, fromDate: item.fromDate, toDate: item.toDate, pinnedOnly: Boolean(item.pinnedOnly) }
  })
}

export function useSavedNoteViews() {
  const [views, setViews] = useState<SavedNoteView[]>([])
  const [error, setError] = useState('')
  useEffect(() => {
    const refresh = () => {
      try { setViews(readViews()); setError('') }
      catch { setError('保存视图暂时无法读取，原有数据未改写。') }
    }
    const onStorage = (event: StorageEvent) => { if (event.key === storageKey || event.key === null) refresh() }
    refresh()
    window.addEventListener('storage', onStorage)
    window.addEventListener(changedEvent, refresh)
    return () => { window.removeEventListener('storage', onStorage); window.removeEventListener(changedEvent, refresh) }
  }, [])

  const mutate = (change: (current: SavedNoteView[]) => SavedNoteView[]) => {
    try {
      // Read again before each write so edits from another tab are retained.
      const current = readViews()
      const next = change(current)
      localStorage.setItem(storageKey, JSON.stringify(next))
      setViews(next); setError('')
      window.dispatchEvent(new Event(changedEvent))
      return true
    } catch (cause) {
      setError(cause instanceof ViewChangeError ? cause.message : '保存视图失败，请检查浏览器存储后重试。')
      return false
    }
  }
  const save = (name: string, selection: NoteViewSelection) => mutate(current => {
    if (!hasSelection(selection) || !dateRange(selection.fromDate, selection.toDate) || (selection.tag !== '' && !validTag(selection.tag)) ||
      !name.trim() || Array.from(name.trim()).length > 64) throw new ViewChangeError('请检查视图名称与筛选条件。')
    if (current.some(view => selectionKey(view) === selectionKey(selection))) throw new ViewChangeError('这组筛选条件已经保存。')
    if (current.length >= maxViews) throw new ViewChangeError('最多保存30个视图，请先删除不再使用的视图。')
    return [...current, { ...selection, id: crypto.randomUUID(), name: name.trim() }]
  })
  const rename = (id: string, name: string) => mutate(current => {
    if (!current.some(view => view.id === id)) throw new ViewChangeError('这个视图已在另一个页面删除。')
    if (!name.trim() || Array.from(name.trim()).length > 64) throw new ViewChangeError('视图名称需要1至64个字符。')
    return current.map(view => view.id === id ? { ...view, name: name.trim() } : view)
  })
  const remove = (id: string) => mutate(current => current.filter(view => view.id !== id))
  return { views, error, save, rename, remove }
}
