import { sha256 } from '@noble/hashes/sha2.js'
import type { TextAnchor } from '../api/types'

export async function hashArticle(text: string): Promise<string> {
  const bytes = sha256(new TextEncoder().encode(text))
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

export function anchorSelection(root: HTMLElement, text: string, sourceHash: string): TextAnchor | null {
  const selection = window.getSelection()
  if (!sourceHash || !selection || selection.rangeCount !== 1 || selection.isCollapsed) return null
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  const isTranslation = (node: Node) => (node instanceof Element ? node : node.parentElement)?.closest('[data-translation]')
  if (isTranslation(range.startContainer) || isTranslation(range.endContainer)) return null
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
  if (text.slice(start, end) !== quote || end > 2_000_000) return null
  return { quote, start, end, prefix: Array.from(text.slice(0, start)).slice(-64).join(''), suffix: Array.from(text.slice(end)).slice(0, 64).join(''), sourceHash }
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
