import type { SubtitleDocument } from '../../shared/api/types'

const storageKey = 'tangentry.subtitle-cache.v1'
const lifetime = 30 * 60 * 1000
const maxEntries = 8
const maxCharacters = 2_000_000
type Entry = { document: SubtitleDocument; expires: number }
let entries: Map<string, Entry> | undefined

function validDocument(value: unknown): value is SubtitleDocument {
  if (!value || typeof value !== 'object') return false
  const doc = value as SubtitleDocument
  if (doc.status !== 'available' || typeof doc.videoId !== 'string' || doc.videoId.length > 160 || !Number.isInteger(doc.part) || doc.part < 1 ||
      !Array.isArray(doc.tracks) || !doc.tracks.length || doc.tracks.length > 64) return false
  let total = 0
  const validLanguage = (value: { language: string; label: string; automatic?: boolean }) =>
    value && typeof value.language === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(value.language) &&
    typeof value.label === 'string' && value.label.length <= 200 && (value.automatic === undefined || typeof value.automatic === 'boolean')
  if (doc.languages !== undefined && (!Array.isArray(doc.languages) || doc.languages.length > 64 || !doc.languages.every(validLanguage))) return false
  return doc.tracks.every(track => {
    if (!validLanguage(track) || !Array.isArray(track.cues) || !track.cues.length || (total += track.cues.length) > 10000) return false
    let previous = -1
    return track.cues.every(cue => {
      const valid = cue && Number.isFinite(cue.start) && Number.isFinite(cue.end) && cue.start >= 0 && cue.start >= previous &&
        cue.end > cue.start && cue.end <= 604800 && typeof cue.text === 'string' && cue.text.trim().length > 0 && cue.text.length <= 16000
      previous = cue?.start
      return valid
    })
  })
}

function cache(): Map<string, Entry> {
  if (entries) return entries
  entries = new Map()
  try {
    const raw = sessionStorage.getItem(storageKey)
    if (!raw || raw.length > maxCharacters) return entries
    const values: unknown = JSON.parse(raw)
    if (!Array.isArray(values) || values.length > maxEntries) return entries
    for (const value of values) {
      if (!Array.isArray(value) || value.length !== 2) continue
      const [key, entry] = value
      if (typeof key !== 'string' || !key.startsWith('/subtitles/') || key.length > 1024 || !entry ||
          !Number.isFinite(entry.expires) || entry.expires <= Date.now() || entry.expires > Date.now() + lifetime || !validDocument(entry.document)) continue
      entries.set(key, entry)
    }
  } catch { /* Storage denial or malformed data must not prevent reading. */ }
  return entries
}

function persist() {
  const current = cache()
  for (const [key, entry] of current) if (entry.expires <= Date.now()) current.delete(key)
  while (current.size > maxEntries) current.delete(current.keys().next().value!)
  let raw = JSON.stringify([...current])
  while (raw.length > maxCharacters && current.size) {
    current.delete(current.keys().next().value!)
    raw = JSON.stringify([...current])
  }
  try {
    if (current.size) sessionStorage.setItem(storageKey, raw)
    else sessionStorage.removeItem(storageKey)
  } catch { /* Keep a bounded memory cache when browser storage is unavailable. */ }
}

export function cachedSubtitles(key: string, videoId: string): SubtitleDocument | undefined {
  const current = cache()
  const entry = current.get(key)
  if (!entry || entry.expires <= Date.now() || entry.document.videoId !== videoId) {
    if (entry) { current.delete(key); persist() }
    return undefined
  }
  current.delete(key)
  current.set(key, entry)
  return entry.document
}

export function cacheSubtitles(key: string, document: SubtitleDocument) {
  if (!validDocument(document)) return
  // Store only the public caption contract, never arbitrary response fields.
  const language = (value: { language: string; label: string; automatic?: boolean }) =>
    ({ language: value.language, label: value.label, automatic: value.automatic ?? false })
  const clean: SubtitleDocument = {
    videoId: document.videoId, part: document.part, status: document.status,
    languages: document.languages?.map(language),
    tracks: document.tracks.map(track => ({ ...language(track), cues: track.cues.map(cue => ({ start: cue.start, end: cue.end, text: cue.text })) })),
  }
  cache().delete(key)
  cache().set(key, { document: clean, expires: Date.now() + lifetime })
  persist()
}

export function clearSubtitleCache(videoId: string) {
  for (const [key, entry] of cache()) if (entry.document.videoId === videoId) cache().delete(key)
  persist()
}
