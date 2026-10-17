import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem } from '../../shared/api/types'

export interface FavoritePolicy { enabled: boolean; autoDownload: boolean; intervalMinutes: number; maxPerRun: number; maxHeight: number; titleInclude?: string; titleExclude?: string; authorInclude?: string }
export interface FavoriteSource { folderId: string; title: string; policy: FavoritePolicy; lastAttemptAt: number; lastSuccessAt: number; message: string; active: boolean; itemCount: number; pendingCount: number; excluded: number }
interface Detail { folderId: string; items: ContentItem[] }
interface Folder { id: string; title: string; count: number }
export const followPolicy = (autoDownload: boolean): FavoritePolicy => ({ enabled: true, autoDownload, intervalMinutes: 60, maxPerRun: 3, maxHeight: 1080 })

// Inactive pages keep the last state but stop reading and polling.
export function useFavoriteSync(enabled = true) {
  const [sources, setSources] = useState<FavoriteSource[]>([])
  const [selected, setSelected] = useState('')
  const [detail, setDetail] = useState<Detail | null>(null)
  const [folders, setFolders] = useState<Folder[]>([])
  const [error, setError] = useState('')
  const [detailError, setDetailError] = useState('')
  const [folderError, setFolderError] = useState('')
  const [loading, setLoading] = useState(true)
  const [readingFolders, setReadingFolders] = useState(false)
  const [pending, setPending] = useState(false)
  const [notice, setNotice] = useState('')
  const reading = useRef<AbortController | null>(null)
  const mutation = useRef(false)
  const reload = useCallback(async (background = false) => {
    if (background && (reading.current || mutation.current)) return
    reading.current?.abort()
    const controller = new AbortController(); reading.current = controller
    try {
      const result = await request<{ sources: FavoriteSource[] }>('/favorite-sync', { signal: controller.signal })
      if (!controller.signal.aborted) { setSources(result.sources); setError(''); setLoading(false); setSelected(current => result.sources.some(source => source.folderId === current) ? current : result.sources[0]?.folderId ?? '') }
    } catch (cause) { if (!controller.signal.aborted) { setError(cause instanceof Error ? cause.message : '收藏同步暂时无法读取。'); setLoading(false) } }
    finally { if (reading.current === controller) reading.current = null }
  }, [])
  useEffect(() => { if (!enabled) return; void reload(); return () => reading.current?.abort() }, [reload, enabled])
  const active = sources.some(source => source.active)
  const scheduled = sources.some(source => source.policy.enabled)
  useEffect(() => {
    if (!enabled || !active && !scheduled) return
    const timer = window.setInterval(() => { if (!document.hidden) void reload(true) }, active ? 1500 : 30000)
    return () => window.clearInterval(timer)
  }, [active, scheduled, reload, enabled])
  useEffect(() => {
    if (!enabled) return
    const refresh = () => { if (!document.hidden) void reload(true) }
    window.addEventListener('focus', refresh); document.addEventListener('visibilitychange', refresh)
    return () => { window.removeEventListener('focus', refresh); document.removeEventListener('visibilitychange', refresh) }
  }, [reload, enabled])
  const source = sources.find(item => item.folderId === selected)
  useEffect(() => {
    if (!enabled) return
    if (!selected) { setDetail(null); return }
    const controller = new AbortController()
    setDetailError('')
    if (detail?.folderId !== selected) setDetail(null)
    void request<Detail>(`/favorite-sync/${encodeURIComponent(selected)}`, { signal: controller.signal }).then(value => { if (!controller.signal.aborted) setDetail(value) }).catch(cause => { if (!controller.signal.aborted) setDetailError(cause instanceof Error ? cause.message : '收藏缓存无法读取。') })
    return () => controller.abort()
  // Cached details refresh on completion, without replacing local policy drafts.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, selected, source?.active, source?.lastSuccessAt, source?.itemCount])
  async function change(path: string, method: string, body?: unknown): Promise<boolean> {
    if (mutation.current) return false
    mutation.current = true; setPending(true); setError('')
    try { const result = await request<{ folderId: string } | undefined>(path, { method, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }); if (path === '/favorite-sync' && method === 'POST' && result) setSelected(result.folderId); await reload(); return true }
    catch (cause) { setError(cause instanceof Error ? cause.message : '配置没有保存，请重试。'); return false }
    finally { mutation.current = false; setPending(false) }
  }
  // One step from a folder to a synced collection: save it, schedule it, and read it now.
  async function follow(query: string, autoDownload: boolean): Promise<string | null> {
    if (mutation.current) return null
    mutation.current = true; setPending(true); setError(''); setNotice('')
    try {
      const policy = followPolicy(autoDownload)
      const created = await request<{ folderId: string; policy: FavoritePolicy }>('/favorite-sync', { method: 'POST', body: JSON.stringify({ query, policy }) })
      if (!created.policy.enabled || created.policy.autoDownload !== autoDownload) await request(`/favorite-sync/${encodeURIComponent(created.folderId)}`, { method: 'PUT', body: JSON.stringify({ ...created.policy, enabled: true, autoDownload }) })
      try { await request(`/favorite-sync/${encodeURIComponent(created.folderId)}/sync`, { method: 'POST' }) }
      catch (cause) { setNotice(`已启用同步。${cause instanceof Error ? cause.message : ''} 稍后会自动重试。`) }
      setSelected(created.folderId)
      await reload()
      return created.folderId
    } catch (cause) { setError(cause instanceof Error ? cause.message : '收藏夹没有保存，请重试。'); return null }
    finally { mutation.current = false; setPending(false) }
  }
  async function readFolders() {
    if (readingFolders) return
    setReadingFolders(true); setFolderError('')
    try { const result = await request<{ folders: Folder[] }>('/favorite-folders'); setFolders(result.folders) }
    catch (cause) { setFolderError(cause instanceof Error ? cause.message : '收藏夹无法读取，请检查 B站 Cookie。') }
    finally { setReadingFolders(false) }
  }
  return { sources, selected, setSelected, source, detail, folders, error, notice, folderError, detailError, loading, pending, active, readingFolders, readFolders, reload, follow,
    create: (query: string) => change('/favorite-sync', 'POST', { query }),
    configure: (id: string, policy: FavoritePolicy) => change(`/favorite-sync/${encodeURIComponent(id)}`, 'PUT', policy),
    start: (id: string) => change(`/favorite-sync/${encodeURIComponent(id)}/sync`, 'POST'),
    cancel: (id: string) => change(`/favorite-sync/${encodeURIComponent(id)}/cancel`, 'POST'),
    remove: (id: string) => change(`/favorite-sync/${encodeURIComponent(id)}`, 'DELETE') }
}
