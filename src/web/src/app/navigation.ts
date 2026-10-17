import type { ContentItem } from '../shared/api/types'

export type View = 'discovery' | 'podcasts' | 'bilibili' | 'search' | 'library' | 'subscriptions' | 'downloads' | 'tools' | 'reader' | 'notes' | 'history'
export const viewPaths: Record<View, string> = { bilibili: '/bilibili', history: '/history', discovery: '/', podcasts: '/podcasts', search:'/search', library: '/library', subscriptions: '/subscriptions', downloads: '/downloads', tools: '/tools', reader: '/reader', notes: '/notes' }
export const viewNames: Record<View, string> = { bilibili: 'B站', history: '历史', discovery: '发现', podcasts: '播客', search:'添加内容', library: '收藏', subscriptions: '订阅', downloads: '下载', tools: '设置', reader: '阅读与播放', notes: '笔记' }
export const viewFromURL = (): View => Object.entries(viewPaths).find(([, path]) => path === location.pathname)?.[0] as View ?? 'discovery'
export const readerQueryFromURL = () => new URLSearchParams(location.search).get('video') ?? new URLSearchParams(location.search).get('contentId') ?? ''
export const readerNoteFromURL = () => {
  const id = new URLSearchParams(location.search).get('note') ?? ''
  return /^[a-f0-9]{32}$/.test(id) ? id : ''
}
export function readerPath(contentId: string, noteId = '') {
  const params = /^bilibili:BV[0-9A-Za-z]{10}$/.test(contentId)
    ? new URLSearchParams({ video: contentId.slice('bilibili:'.length) })
    : new URLSearchParams({ contentId })
  if (/^[a-f0-9]{32}$/.test(noteId)) params.set('note', noteId)
  return `${viewPaths.reader}?${params}`
}

/** Build the reader URL for a feed item without treating non-video B站 IDs as BV IDs. */
export function readerPathForItem(item: Pick<ContentItem, 'id' | 'platform' | 'platformId' | 'kind'>) {
  const params = item.platform === 'bilibili' && item.kind === 'video' && /^BV[0-9A-Za-z]{10}$/.test(item.platformId)
    ? new URLSearchParams({ video: item.platformId })
    : new URLSearchParams({ contentId: item.id })
  return `${viewPaths.reader}?${params}`
}
