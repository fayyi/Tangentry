import type { ContentItem } from '../api/types'

const key = ['tangentry', 'note-details', 'v2'].join('.')
type Entry = { item: ContentItem; expires: number }
const memory = new Map<string, Entry>()

// An explicitly supplied new access locator must be fetched before claiming
// that an older detail cache represents that source.
export function sameNoteLocator(saved: string, requested: string): boolean {
  try {
    const previous = new URL(saved), current = new URL(requested)
    const token = current.searchParams.get('xsec_token')
    return !token || token === previous.searchParams.get('xsec_token') &&
      current.searchParams.get('xsec_source') === previous.searchParams.get('xsec_source')
  } catch { return false }
}

function validEntry(value: unknown): value is Entry {
  if (!value || typeof value !== 'object') return false
  const { item, expires } = value as Partial<Entry>
  if (typeof expires !== 'number' || !Number.isFinite(expires) || expires <= Date.now()) return false
  if (!item || typeof item !== 'object' || item.platform !== 'xiaohongshu') return false
  if (!['id', 'platformId', 'title', 'author', 'authorId', 'url', 'thumbnailUrl', 'description']
    .every(field => typeof item[field as keyof ContentItem] === 'string')) return false
  if (!/^[a-f\d]{24}$/i.test(item.platformId) || item.id !== `xiaohongshu:${item.platformId}` || !['image', 'video', 'article'].includes(item.kind)) return false
  if (!Number.isFinite(item.durationSeconds) || !Number.isFinite(item.publishedAt)) return false
  if (!['mediaUrl', 'authorAvatarUrl', 'publishedText'].every(field => item[field as keyof ContentItem] === undefined || typeof item[field as keyof ContentItem] === 'string')) return false
  return item.images === undefined || (Array.isArray(item.images) &&
    item.images.every(image => image && typeof image.url === 'string' &&
      (image.alt === undefined || typeof image.alt === 'string') &&
      [image.width, image.height].every(size => size === undefined || typeof size === 'number' && Number.isFinite(size) && size >= 0)))
}

function remember(entry: Entry) {
  memory.delete(entry.item.id)
  memory.set(entry.item.id, entry)
  while (memory.size > 32) memory.delete(memory.keys().next().value!)
}

function entries(): Entry[] {
  try {
    const data: unknown = JSON.parse(sessionStorage.getItem(key) ?? '[]')
    return Array.isArray(data) ? data.filter(validEntry) : []
  } catch { return [] }
}

export function cachedNote(id: string): ContentItem | undefined {
  const entry = memory.get(id) ?? entries().find(entry => entry.item.id === id)
  if (!entry || entry.expires <= Date.now()) { memory.delete(id); return undefined }
  remember(entry)
  return entry.item
}

export function cacheNote(item: ContentItem) {
  const retained = entries().filter(entry => entry.item.id !== item.id).slice(-31)
  retained.push({ item, expires: Date.now() + (item.kind === 'video' ? 5 * 60 * 1000 : 24 * 60 * 60 * 1000) })
  remember(retained[retained.length - 1])
  try { sessionStorage.setItem(key, JSON.stringify(retained)) } catch { /* Reading also works without browser storage. */ }
}
