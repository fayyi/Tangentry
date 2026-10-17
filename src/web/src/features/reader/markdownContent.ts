const markdownSignals = [
  /(^|\n)\s{0,3}#{1,6}\s+\S/m,
  /(^|\n)\s{0,3}(?:[-*+]\s+|\d+[.)]\s+)/m,
  /(^|\n)\s*>\s+/m,
  /\*\*[^\n]+?\*\*|__[^\n]+?__|(?<!\*)\*[^\n*]+?\*(?!\*)|(?<!_)_[^\n_]+?_(?!_)/,
  /`{1,3}[^`\n]+`{1,3}|\[.+?\]\(https?:\/\/[^)]+\)/,
  /!\[[^\]]*\]\(\s*(?:https?:\/\/|\/\/)[^)]+\)/,
  /(^|\n)\s*\|.+\|\s*\n\s*\|?\s*:?-{3,}/m,
]

export function isLikelyMarkdown(value: string): boolean {
  const text = value.trim()
  if (!text || /<\/?[a-z][^>]*>/i.test(text)) return false
  return markdownSignals.some(pattern => pattern.test(text))
}

// Keep annotation offsets aligned with what the reader visibly renders.
// This is deliberately conservative; unsupported Markdown syntax remains text.
export function markdownToPlainText(value: string): string {
  return value
    .replace(/```[\w-]*\n?([\s\S]*?)```/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s{0,3}(?:[-*+]\s+|\d+[.)]\s+)/gm, '')
    .replace(/^\s*\|\s?/gm, '')
    .replace(/\s*\|\s*$/gm, '')
    .replace(/^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+$/gm, '')
    .replace(/\*\*([^*]+)\*\*|__([^_]+)__|\*([^*]+)\*|_([^_]+)_/g, '$1$2$3$4')
}
