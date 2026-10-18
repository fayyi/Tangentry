import { sha256 } from '@noble/hashes/sha2.js'
import type { TextAnchor } from '../api/types'

export async function hashArticle(text: string): Promise<string> {
  const bytes = sha256(new TextEncoder().encode(text))
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

export function translatedTextAnchor(text: string, sourceHash: string, startValue: string | undefined, endValue: string | undefined): TextAnchor | null {
  const start = Number(startValue)
  const end = Number(endValue)
  if (!sourceHash || !Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end <= start || end > text.length) return null
  const quote = text.slice(start, end)
  if (!quote) return null
  return { quote, start, end, prefix: Array.from(text.slice(0, start)).slice(-64).join(''), suffix: Array.from(text.slice(end)).slice(0, 64).join(''), sourceHash }
}

export function anchorSelection(root: HTMLElement, text: string, sourceHash: string): TextAnchor | null {
  const selection = window.getSelection()
  if (!sourceHash || !selection || selection.rangeCount !== 1 || selection.isCollapsed) return null
  return anchorRange(root, text, sourceHash, selection.getRangeAt(0))
}

export function anchorRange(root: HTMLElement, text: string, sourceHash: string, range: Range): TextAnchor | null {
  if (!sourceHash || range.collapsed) return null
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  const translation = (node: Node) => (node instanceof Element ? node : node.parentElement)?.closest<HTMLElement>('[data-translation]')
  const translatedStart = translation(range.startContainer)
  const translatedEnd = translation(range.endContainer)
  if (translatedStart || translatedEnd) {
    if (!translatedStart || translatedStart !== translatedEnd) return null
    return translatedTextAnchor(text, sourceHash, translatedStart.dataset.sourceStart, translatedStart.dataset.sourceEnd)
  }
  const articleOffset = (container: Node, offset: number, end: boolean) => {
    let boundary = container
    let boundaryOffset = offset
    if (container instanceof Element) {
      const childIndex = end ? offset - 1 : offset
      const child = container.childNodes[childIndex]
      if (child) { boundary = child; boundaryOffset = end && child.nodeType === Node.TEXT_NODE ? child.textContent?.length ?? 0 : 0 }
    }
    const element = (boundary instanceof Element ? boundary : boundary.parentElement)?.closest<HTMLElement>('[data-article-start][data-article-end]')
    if (!element || !root.contains(element)) return null
    const start = Number(element.dataset.articleStart)
    const endOffset = Number(element.dataset.articleEnd)
    if (!Number.isInteger(start) || !Number.isInteger(endOffset) || endOffset < start || !element.contains(boundary)) return null
    const prefix = document.createRange()
    prefix.selectNodeContents(element)
    try { prefix.setEnd(boundary, boundaryOffset) } catch { return null }
    return Math.min(endOffset, start + prefix.toString().length)
  }
  const markedStart = articleOffset(range.startContainer, range.startOffset, false)
  const markedEnd = articleOffset(range.endContainer, range.endOffset, true)
  if (markedStart !== null && markedEnd !== null && markedEnd >= markedStart) {
    const selected = text.slice(markedStart, markedEnd)
    const quote = selected.trim()
    if (quote) {
      const start = markedStart + selected.length - selected.trimStart().length
      return { quote, start, end: start + quote.length, prefix: Array.from(text.slice(0, start)).slice(-64).join(''), suffix: Array.from(text.slice(start + quote.length)).slice(0, 64).join(''), sourceHash }
    }
  }
  const originalText = (value: Range) => {
    const fragment = value.cloneContents()
    fragment.querySelectorAll('[data-translation]').forEach(node => node.remove())
    return fragment.textContent ?? ''
  }
  const before = range.cloneRange()
  before.selectNodeContents(root)
  before.setEnd(range.startContainer, range.startOffset)
  const raw = originalText(range)
  const quote = Array.from(raw.trim()).slice(0, 2000).join('')
  if (!quote) return null
  const start = originalText(before).length + raw.length - raw.trimStart().length
  const end = start + quote.length
  const exact = text.slice(start, end) === quote && end <= 2_000_000 ? { start, end } : null
  const first = text.indexOf(quote)
  const resolved = exact ?? (first >= 0 && text.indexOf(quote, first + 1) < 0 ? { start: first, end: first + quote.length } : null)
  if (!resolved || resolved.end > 2_000_000) return null
  return { quote, start: resolved.start, end: resolved.end, prefix: Array.from(text.slice(0, resolved.start)).slice(-64).join(''), suffix: Array.from(text.slice(resolved.end)).slice(0, 64).join(''), sourceHash }
}

export function resolveTextAnchor(text: string, sourceHash: string, anchor: TextAnchor): { start: number; end: number } | null {
  if (!anchor.quote || !sourceHash) return null
  if (anchor.sourceHash === sourceHash && text.slice(anchor.start, anchor.end) === anchor.quote) return { start: anchor.start, end: anchor.end }
  let match: { start: number; end: number } | null = null
  let offset = 0
  while (offset <= text.length) {
    const start = text.indexOf(anchor.quote, offset)
    if (start < 0) break
    const end = start + anchor.quote.length
    if (text.slice(0, start).endsWith(anchor.prefix) && text.slice(end).startsWith(anchor.suffix)) {
      if (match) return null // An ambiguous match must never highlight a guessed passage.
      match = { start, end }
    }
    offset = start + 1
  }
  return match
}

export function highlightSegments(text: string, ranges: { id: string; start: number; end: number }[], boundaries: number[] = []) {
  const edges = [...new Set([0, text.length, ...ranges.flatMap(range => [range.start, range.end]), ...boundaries])].sort((a, b) => a - b)
  return edges.slice(0, -1).map((start, index) => {
    const end = edges[index + 1]
    return { start, text: text.slice(start, end), ids: ranges.filter(range => range.start <= start && range.end >= end).map(range => range.id) }
  })
}
