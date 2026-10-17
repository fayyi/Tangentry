import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem } from '../../shared/api/types'
import { uploadLegacyRecords } from '../../shared/browser/legacyUpload'

export type HistoryEntry = { item: ContentItem; openedAt: number; openCount?: number }
// Earlier versions kept history in this browser storage key.
export const viewingHistoryStorageKey = 'tangentry.viewing-history.v1'
const maxEntries = 1000
// Other windows reload after a write, as browser storage events once did.
const channelName = 'tangentry.viewing-history'

function readLegacy(): HistoryEntry[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(viewingHistoryStorageKey) || '[]')
    if (!Array.isArray(value)) return []
    const seen = new Set<string>()
    return value.filter(entry => {
      const item = entry?.item
      if (!item || typeof item !== 'object' || typeof entry.openedAt !== 'number' ||
        !Number.isFinite(entry.openedAt) || entry.openedAt < 0 || entry.openedAt > Date.now() + 86400000) return false
      if (!['id', 'title', 'platform', 'platformId', 'author', 'authorId', 'url', 'thumbnailUrl', 'description']
        .every(field => typeof item[field] === 'string')) return false
      if (!item.id || !['video', 'article', 'image', 'audio', 'pdf'].includes(item.kind) ||
        !Number.isFinite(item.durationSeconds) || !Number.isFinite(item.publishedAt)) return false
      if (!['mediaUrl', 'authorAvatarUrl', 'publishedText'].every(field => item[field] === undefined || typeof item[field] === 'string')) return false
      if (item.images !== undefined && (!Array.isArray(item.images) || !item.images.every((image: unknown) => {
        if (!image || typeof image !== 'object') return false
        const value = image as Record<string, unknown>
        return typeof value.url === 'string' && (value.alt === undefined || typeof value.alt === 'string') &&
          ['width', 'height'].every(field => value[field] === undefined || typeof value[field] === 'number' && Number.isFinite(value[field]) && value[field] >= 0)
      }))) return false
      return true
    }).sort((a, b) => b.openedAt - a.openedAt).filter(entry => {
      if (seen.has(entry.item.id)) return false
      seen.add(entry.item.id)
      return true
    })
  } catch { return [] }
}

// The server keeps the same bounded snapshot; mirror it for the optimistic list.
function snapshot(item: ContentItem): ContentItem {
  const { articleBlocks: _blocks, partial: _partial, ...card } = item
  return { ...card, description: item.description?.slice(0, 1200), images: item.images?.slice(0, 1) }
}

export function useViewingHistory() {
  const [entries, setEntries] = useState<HistoryEntry[]>([])
  const [error, setError] = useState('')
  // A write invalidates reads that started before it.
  const generation = useRef(0)
  const load = useCallback(async () => {
    const token = ++generation.current
    try {
      const result = await request<{ entries: HistoryEntry[] }>('/history/viewing')
      if (token === generation.current) { setEntries(result.entries); setError('') }
    } catch { if (token === generation.current) setError('暂时无法读取历史记录。') }
  }, [])
  useEffect(() => {
    void uploadLegacyRecords(viewingHistoryStorageKey, readLegacy, values =>
      request('/history/viewing', { method: 'POST', body: JSON.stringify({ entries: values.map(entry => ({ item: snapshot(entry.item), openedAt: entry.openedAt })) }) }),
    ).then(load)
    const refresh = () => { if (document.visibilityState === 'visible') void load() }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    const channel = typeof BroadcastChannel === 'undefined' ? undefined : new BroadcastChannel(channelName)
    if (channel) channel.onmessage = () => { void load() }
    return () => {
      channel?.close()
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [load])
  const write = useCallback((change: (entries: HistoryEntry[]) => HistoryEntry[], send: () => Promise<unknown>) => {
    ++generation.current
    setEntries(change)
    setError('')
    send().then(() => {
      if (typeof BroadcastChannel === 'undefined') return
      const channel = new BroadcastChannel(channelName)
      channel.postMessage('changed')
      channel.close()
    }, () => setError('历史记录未能保存，请确认 Tangentry 仍在运行。'))
  }, [])
  const record = useCallback((item: ContentItem) => {
    const entry = { item: snapshot(item), openedAt: Date.now() }
    write(entries => [{ ...entry, openCount: (entries.find(value => value.item.id === item.id)?.openCount ?? 0) + 1 }, ...entries.filter(value => value.item.id !== item.id)].slice(0, maxEntries),
      () => request('/history/viewing', { method: 'POST', body: JSON.stringify({ entries: [entry] }) }))
  }, [write])
  const remove = useCallback((id: string) => write(entries => entries.filter(entry => entry.item.id !== id),
    () => request(`/history/viewing/${encodeURIComponent(id)}`, { method: 'DELETE' })), [write])
  const clear = useCallback(() => write(() => [], () => request('/history/viewing', { method: 'DELETE' })), [write])
  return { entries, error, record, remove, clear }
}
