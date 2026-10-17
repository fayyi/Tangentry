import { request } from '../api/client'
import type { ContentItem } from '../api/types'

type Publication = Pick<ContentItem, 'publishedAt' | 'publishedText' | 'authorAvatarUrl'> & { avatarExpires?: number; description?: string; descriptionExpires?: number; thumbnailUrl?: string; coverExpires?: number }
const memory = new Map<string, { value: Publication; expires: number }>()
const pending = new Map<string, Promise<Publication | null>>()
let active = 0
const waiting: (() => void)[] = []

export function cachedPublication(id: string): Publication | null {
  const remembered = memory.get(id)
  if (remembered && remembered.expires > Date.now()) return remembered.value
  memory.delete(id)
  try {
    const current = localStorage.getItem(`tangentry.publication.v2.${id}`)
    const value = JSON.parse(current ?? localStorage.getItem(`tangentry.publication.v1.${id}`) ?? 'null')
    // Keep precise legacy dates, but discard labels stored under the old month-long policy.
    if (!current && !value?.publishedAt) return null
    if (value && Number.isFinite(value.publishedAt) && value.publishedAt >= 0 && value.publishedAt <= 253402300799 &&
      (value.publishedText === undefined || typeof value.publishedText === 'string') &&
      (value.description === undefined || typeof value.description === 'string') &&
      (value.thumbnailUrl === undefined || typeof value.thumbnailUrl === 'string') && value.expires > Date.now()) return value
  } catch { /* Storage is optional. */ }
  return null
}

function rememberPublication(id: string, value: Publication, ttl: number): Publication {
  const expires = Date.now() + ttl
  memory.delete(id)
  memory.set(id, { value, expires })
  while (memory.size > 500) memory.delete(memory.keys().next().value!)
  try {
    localStorage.setItem(`tangentry.publication.v2.${id}`, JSON.stringify({ ...value, expires }))
  } catch { /* Memory caching remains available without browser storage. */ }
  return value
}

export function rememberPublicationItem(item: ContentItem) {
  const cached = cachedPublication(item.id)
  const expires = Date.now() + 300000
  return rememberPublication(item.id, {
    ...cached,
    publishedAt: item.publishedAt || cached?.publishedAt || 0,
    publishedText: item.publishedText || cached?.publishedText,
    authorAvatarUrl: item.authorAvatarUrl || cached?.authorAvatarUrl,
    description: item.description || cached?.description,
    thumbnailUrl: item.thumbnailUrl || cached?.thumbnailUrl,
    avatarExpires: expires,
    descriptionExpires: expires,
    coverExpires: expires,
  }, 300000)
}

export function readPublication(item: ContentItem, requireAvatar = false, requireDescription = false, requireCover = false): Promise<Publication | null> {
  const cached = cachedPublication(item.id)
  if (cached && (!requireAvatar || (cached.avatarExpires ?? 0) > Date.now()) &&
    (!requireDescription || (cached.descriptionExpires ?? 0) > Date.now()) &&
    (!requireCover || (cached.coverExpires ?? 0) > Date.now())) return Promise.resolve(cached)
  const existing = pending.get(item.id)
  if (existing) return existing
  const task = (async () => {
    if (active >= 2) await new Promise<void>(resolve => waiting.push(resolve))
    else active++
    try {
      const feed = await request<{ items: ContentItem[] }>(`/discovery/${item.platform}?q=${encodeURIComponent(item.url)}`, { signal: AbortSignal.timeout(30000) })
      const detail = feed.items.find(value => value.id === item.id)
      if (!detail) throw new Error('Invalid publication details')
      const publishedAt = detail.publishedAt
      const validDate = Number.isFinite(publishedAt) && publishedAt >= 0 && publishedAt <= 253402300799
      const avatar = typeof detail.authorAvatarUrl === 'string' ? detail.authorAvatarUrl.trim() : ''
      const publishedText = typeof detail.publishedText === 'string' ? detail.publishedText.trim() : ''
      const descriptionValue = typeof detail.description === 'string' ? detail.description.trim() : ''
      const thumbnailUrl = typeof detail.thumbnailUrl === 'string' ? detail.thumbnailUrl.trim() : ''
      // A platform may expose the avatar or description while withholding a
      // publication date. Keep those independent fields instead of dropping
      // the whole response and leaving a permanent placeholder.
      if (!validDate && !avatar && !publishedText && !descriptionValue && !thumbnailUrl) throw new Error('Invalid publication details')
      const description = descriptionValue || cached?.description || ''
      const result: Publication = { publishedAt: validDate && publishedAt ? publishedAt : cached?.publishedAt || 0, publishedText: publishedText || cached?.publishedText, authorAvatarUrl: avatar || cached?.authorAvatarUrl || '', avatarExpires: Date.now() + (avatar ? 86400000 : 300000), description, descriptionExpires: Date.now() + (description ? 86400000 : 300000), thumbnailUrl: thumbnailUrl || cached?.thumbnailUrl, coverExpires: Date.now() + (thumbnailUrl ? 86400000 : 300000) }
      // Relative platform labels age even when the underlying video is unchanged.
      return rememberPublication(item.id, result, result.publishedAt ? 30 * 86400000 : result.publishedText ? 3600000 : 300000)
    } catch { return rememberPublication(item.id, { ...cached, publishedAt: cached?.publishedAt ?? 0, authorAvatarUrl: cached?.authorAvatarUrl ?? '', avatarExpires: Date.now() + 60000, descriptionExpires: Date.now() + 60000, coverExpires: Date.now() + 60000 }, cached?.publishedAt ? 30 * 86400000 : 60000) }
    finally {
      const next = waiting.shift()
      if (next) next()
      else active--
      pending.delete(item.id)
    }
  })()
  pending.set(item.id, task)
  return task
}
