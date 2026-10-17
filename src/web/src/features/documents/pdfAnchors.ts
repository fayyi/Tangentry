import type { PageViewport } from 'pdfjs-dist'
import type { PDFAnchor, PDFRect } from '../../shared/api/types'

export interface PDFSummarySegment { index: number; start: number; end: number; page: number; text: string }

// Keep PDF extraction independent from the canvas so summary input can be
// tested without pdf.js or a browser DOM.
export function pdfSummarySegments(pages: Array<{ page: number; text: string }>): PDFSummarySegment[] {
  const segments: PDFSummarySegment[] = []
  const encoder = new TextEncoder()
  for (const { page, text } of pages) {
    const value = text.trim()
    let part = ''
    let bytes = 0
    const flush = () => {
      const chunk = part.trim()
      if (chunk) {
        const index = segments.length
        segments.push({ index, start: 0, end: 0, page, text: chunk })
      }
      part = ''
      bytes = 0
    }
    for (const character of value) {
      const size = encoder.encode(character).length
      if (bytes + size > 7000) flush()
      part += character
      bytes += size
      if (character === '\n' && bytes > 5500) flush()
    }
    if (part) flush()
  }
  return segments
}

export function pdfSelection(root: HTMLElement, viewport: PageViewport, documentId: string, page: number): PDFAnchor | null {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) return null
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  const fragments: string[] = [], selectedRects: DOMRect[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  let node: Node | null
  while ((node = walker.nextNode())) {
    if (!range.intersectsNode(node)) continue
    const start = range.startContainer === node ? range.startOffset : 0
    const end = range.endContainer === node ? range.endOffset : node.textContent!.length
    if (end <= start) continue
    fragments.push(node.textContent!.slice(start, end))
    if (node.parentElement?.nextElementSibling?.tagName === 'BR') fragments.push('\n')
    const portion = document.createRange()
    portion.setStart(node, start); portion.setEnd(node, end)
    selectedRects.push(...portion.getClientRects())
  }
  const quote = fragments.join('').trim()
  if (!quote || Array.from(quote).length > 2000) return null
  const bounds = root.parentElement!.getBoundingClientRect()
  const box = viewport.viewBox
  const pageWidth = box[2] - box[0], pageHeight = box[3] - box[1]
  const rects: PDFRect[] = []
  // Whole-run selection also reports span boxes. Use only the selected glyph
  // ranges, avoiding duplicate overlays with different line-box heights.
  for (const rect of selectedRects) {
    if (rect.width < .1 || rect.height < .1) continue
    const a = viewport.convertToPdfPoint(rect.left - bounds.left, rect.top - bounds.top)
    const b = viewport.convertToPdfPoint(rect.right - bounds.left, rect.bottom - bounds.top)
    const left = Math.max(0, Math.min(1, (Math.min(a[0], b[0]) - box[0]) / pageWidth))
    const bottom = Math.max(0, Math.min(1, (Math.min(a[1], b[1]) - box[1]) / pageHeight))
    const right = Math.max(0, Math.min(1, (Math.max(a[0], b[0]) - box[0]) / pageWidth))
    const top = Math.max(0, Math.min(1, (Math.max(a[1], b[1]) - box[1]) / pageHeight))
    if (right <= left || top <= bottom) continue
    const next = { x: left, y: bottom, width: right - left, height: top - bottom }
    if (!rects.some(previous => Object.keys(next).every(key => Math.abs(previous[key as keyof PDFRect] - next[key as keyof PDFRect]) < .000001))) rects.push(next)
  }
  return rects.length > 0 && rects.length <= 200 ? { documentId, page, quote, rects } : null
}

export function viewportPDFRect(viewport: PageViewport, rect: PDFRect) {
  const box = viewport.viewBox
  const width = box[2] - box[0], height = box[3] - box[1]
  const points = [...viewport.convertToViewportPoint(box[0] + rect.x * width, box[1] + rect.y * height), ...viewport.convertToViewportPoint(box[0] + (rect.x + rect.width) * width, box[1] + (rect.y + rect.height) * height)]
  return { left: Math.min(points[0], points[2]), top: Math.min(points[1], points[3]), width: Math.abs(points[2] - points[0]), height: Math.abs(points[3] - points[1]) }
}
