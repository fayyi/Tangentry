import type { SubtitleReference } from '../../shared/api/types'

export function subtitleClock(value: number) {
  const total = Math.floor(value)
  return `${total >= 3600 ? Math.floor(total / 3600) + ':' : ''}${String(Math.floor(total / 60) % 60).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

export function subtitleReferenceValid(value: unknown): value is SubtitleReference {
  if (!value || typeof value !== 'object') return false
  const reference = value as SubtitleReference
  return typeof reference.start === 'number' && Number.isFinite(reference.start) && reference.start >= 0 &&
    typeof reference.end === 'number' && Number.isFinite(reference.end) && reference.end > reference.start && reference.end <= 604800 &&
    typeof reference.text === 'string' && Boolean(reference.text.trim()) && Array.from(reference.text).length <= 2000 &&
    typeof reference.language === 'string' && Boolean(reference.language.trim()) && Array.from(reference.language).length <= 64 &&
    (reference.trackHash === undefined || typeof reference.trackHash === 'string' && /^[a-f0-9]{64}$/.test(reference.trackHash))
}

export function sameSubtitleReference(left: SubtitleReference, right: SubtitleReference) {
  return left.start === right.start && left.end === right.end && left.text === right.text && left.language === right.language && left.trackHash === right.trackHash
}
