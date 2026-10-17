import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import { uploadLegacyRecords } from '../../shared/browser/legacyUpload'
import { discoverySources } from '../discovery/sources'
import type { DiscoverySource } from '../discovery/sources'
// Earlier versions kept search history in this browser storage key.
const key = 'tangentry.search-history.v1'
const maxRecords = 100
export interface SearchRecord { id: string; query: string; source: DiscoverySource; createdAt: number }
function valid(value: unknown): value is SearchRecord {
  if (value === null || typeof value !== 'object') return false
  const record = value as Record<string, unknown>
  return typeof record.id === 'string' && typeof record.query === 'string' && record.query.trim().length > 0 && record.query.length <= 2048 && typeof record.source === 'string' && Object.hasOwn(discoverySources, record.source) && Number.isFinite(record.createdAt)
}
function readLegacy(): SearchRecord[] {
  try {
    const values: unknown = JSON.parse(localStorage.getItem(key) ?? '[]')
    return Array.isArray(values) ? values.filter(valid).slice(0, maxRecords) : []
  } catch { return [] }
}
export function useSearchHistory() {
  const [records, setRecords] = useState<SearchRecord[]>([]), [error, setError] = useState('')
  const generation = useRef(0)
  const load = useCallback(async () => {
    const token = ++generation.current
    try {
      const result = await request<{ records: unknown[] }>('/history/searches')
      if (token === generation.current) { setRecords(result.records.filter(valid)); setError('') }
    } catch { if (token === generation.current) setError('暂时无法读取搜索历史。') }
  }, [])
  useEffect(() => {
    void uploadLegacyRecords(key, readLegacy, values => request('/history/searches', { method: 'POST', body: JSON.stringify({ records: values }) })).then(load)
  }, [load])
  const write = (change: (records: SearchRecord[]) => SearchRecord[], send: () => Promise<unknown>) => {
    ++generation.current
    setRecords(change)
    setError('')
    send().catch(() => setError('搜索历史未能保存，本次会话记录仍可用。'))
  }
  return {
    records, error,
    add: (query: string, source: DiscoverySource) => {
      if (!query.trim()) return
      const record = { id: crypto.randomUUID(), query: query.trim(), source, createdAt: Date.now() }
      write(records => [record, ...records].slice(0, maxRecords), () => request('/history/searches', { method: 'POST', body: JSON.stringify({ records: [record] }) }))
    },
    remove: (id: string) => write(records => records.filter(value => value.id !== id), () => request(`/history/searches/${encodeURIComponent(id)}`, { method: 'DELETE' })),
    clear: () => write(() => [], () => request('/history/searches', { method: 'DELETE' })),
  }
}
