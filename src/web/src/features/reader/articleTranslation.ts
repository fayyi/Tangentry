export function articleTranslationParts(paragraphs: (string | null)[], limit = 7800) {
  const pieces: string[] = []
  const ranges = paragraphs.map(paragraph => {
    const start = pieces.length
    if (paragraph === null) return { start, end: start }
    let part = '', bytes = 0
    const commit = () => { if (part.trim()) pieces.push(part); part = ''; bytes = 0 }
    for (const char of paragraph) {
      const code = char.codePointAt(0)!
      const size = code <= 0x7f ? 1 : code <= 0x7ff ? 2 : code <= 0xffff ? 3 : 4
      if (bytes + size > limit) commit()
      part += char; bytes += size
    }
    commit()
    return { start, end: pieces.length }
  })
  return { pieces, ranges }
}
