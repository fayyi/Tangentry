import type { PDFAnchor, SubtitleReference, TextAnchor } from '../../shared/api/types'
import { sameSubtitleReference, subtitleReferenceValid } from './subtitleReferenceData'
import { capturePosition, validPosition } from './noteTime'

export interface DraftImage { data: string; width: number; height: number; positionSeconds: number }
export interface Draft {
  createId?: string
  images?: DraftImage[]
  /** Legacy session drafts stored one image. */
  image?: DraftImage
  captureFrame?: boolean
  frameSpec?: { downloadId: string; positionSeconds: number }
  expectedUpdatedAt?: number
  tags?: string
  body: string
  positionSeconds: number | null
  editingId: string | null
  timeDetached?: boolean
  textAnchor?: TextAnchor | null
  pdfAnchor?: PDFAnchor | null
  subtitleReferences?: SubtitleReference[]
}

export const emptyDraft: Draft = { body: '', positionSeconds: null, editingId: null, textAnchor: null, pdfAnchor: null }

function parsePosition(raw: string): number | null {
  if (!raw.trim()) return null
  const match = /^(?:(\d{1,3}):)?([0-5]?\d):([0-5]\d)(?:\.(\d{1,3}))?$/.exec(raw.trim())
  if (!match) return NaN
  const value = Number(match[1] ?? 0) * 3600 + Number(match[2]) * 60 + Number(match[3]) + Number(`0.${match[4] ?? 0}`)
  return value <= 7 * 24 * 3600 ? value : NaN
}

function draftAnchor(value: unknown): TextAnchor | null {
  if (!value || typeof value !== 'object') return null
  const anchor = value as TextAnchor
  return typeof anchor.quote === 'string' && Array.from(anchor.quote).length > 0 && Array.from(anchor.quote).length <= 2000 &&
    typeof anchor.prefix === 'string' && Array.from(anchor.prefix).length <= 64 &&
    typeof anchor.suffix === 'string' && Array.from(anchor.suffix).length <= 64 &&
    typeof anchor.sourceHash === 'string' && /^[a-f0-9]{64}$/.test(anchor.sourceHash) &&
    Number.isInteger(anchor.start) && Number.isInteger(anchor.end) && anchor.start >= 0 && anchor.end <= 2_000_000 &&
    anchor.end - anchor.start === anchor.quote.length ? anchor : null
}

function draftPDFAnchor(value: unknown): PDFAnchor | null {
  if (!value || typeof value !== 'object') return null
  const anchor = value as PDFAnchor
  if (typeof anchor.documentId !== 'string' || !/^[a-f0-9]{64}$/.test(anchor.documentId) || !Number.isInteger(anchor.page) || anchor.page < 1 || anchor.page > 100000 ||
    typeof anchor.quote !== 'string' || !anchor.quote || Array.from(anchor.quote).length > 2000 || !Array.isArray(anchor.rects) || anchor.rects.length === 0 || anchor.rects.length > 200) return null
  return anchor.rects.every(rect => rect && [rect.x, rect.y, rect.width, rect.height].every(value => typeof value === 'number' && Number.isFinite(value)) &&
    rect.x >= 0 && rect.y >= 0 && rect.width > 0 && rect.height > 0 && rect.x + rect.width <= 1.000000001 && rect.y + rect.height <= 1.000000001) ? anchor : null
}

function draftImage(value: unknown): DraftImage | undefined {
  if (!value || typeof value !== 'object') return undefined
  const image = value as DraftImage
  return typeof image.data === 'string' && image.data.length > 0 && image.data.length <= 1_400_000 && /^[A-Za-z0-9+/]+={0,2}$/.test(image.data) &&
    Number.isInteger(image.width) && Number.isInteger(image.height) && image.width > 0 && image.height > 0 && image.width <= 1920 && image.height <= 1920 &&
    validPosition(image.positionSeconds) !== null ? image : undefined
}

export function readDraft(key: string, raw?: string): Draft {
  try {
    const cached = JSON.parse(raw ?? sessionStorage.getItem(key) ?? 'null') as (Draft & { time?: string }) | null
    if (cached && typeof cached.body === 'string' && cached.body.length <= 16000 && (cached.editingId === null || /^[a-f0-9]{32}$/.test(cached.editingId))) {
      const value = cached.positionSeconds ?? (typeof cached.time === 'string' ? parsePosition(cached.time) : null)
      if (cached.expectedUpdatedAt !== undefined && (!Number.isSafeInteger(cached.expectedUpdatedAt) || cached.expectedUpdatedAt < 0)) return { ...emptyDraft }
      const subtitleReferences = Array.isArray(cached.subtitleReferences) && cached.subtitleReferences.length <= 20 && cached.subtitleReferences.every(subtitleReferenceValid) && cached.subtitleReferences.reduce((total, reference) => total + Array.from(reference.text).length, 0) <= 8000
        ? cached.subtitleReferences.filter((reference, index, references) => references.findIndex(other => sameSubtitleReference(reference, other)) === index)
        : []
      const createId = typeof cached.createId === 'string' && /^[a-f0-9]{32}$/.test(cached.createId) ? cached.createId : undefined
      const frameSpec = cached.frameSpec && typeof cached.frameSpec.downloadId === 'string' && /^[A-Za-z0-9_-]{16,64}$/.test(cached.frameSpec.downloadId) && validPosition(cached.frameSpec.positionSeconds) !== null ? cached.frameSpec : undefined
      const images = Array.isArray(cached.images) && cached.images.length <= 10 ? cached.images.map(draftImage).filter((image): image is DraftImage => Boolean(image)) : []
      const legacyImage = draftImage(cached.image)
      if (!images.length && legacyImage) images.push(legacyImage)
      return {
        createId, images, captureFrame: cached.captureFrame === true && !images.length, frameSpec, expectedUpdatedAt: cached.expectedUpdatedAt,
        tags: typeof cached.tags === 'string' && cached.tags.length <= 512 ? cached.tags : undefined, body: cached.body, editingId: cached.editingId,
        positionSeconds: cached.editingId || createId ? validPosition(value) : capturePosition(value), timeDetached: cached.timeDetached === true,
        textAnchor: draftAnchor(cached.textAnchor), pdfAnchor: draftAnchor(cached.textAnchor) ? null : draftPDFAnchor(cached.pdfAnchor), subtitleReferences,
      }
    }
  } catch { /* A private draft is optional; notes remain available through the API. */ }
  return { ...emptyDraft }
}
