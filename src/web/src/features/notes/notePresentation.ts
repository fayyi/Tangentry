import type { ReadingNote } from '../../shared/api/types'

export function notePresentation(note: Pick<ReadingNote, 'body' | 'textAnchor' | 'pdfAnchor' | 'subtitleReferences'>) {
  if (note.subtitleReferences?.length) {
    const block = '> ' + note.subtitleReferences[0].text.replaceAll('\n', '\n> ')
    return { quote: '', body: note.body === block ? '' : note.body }
  }
  const quote = note.textAnchor?.quote ?? note.pdfAnchor?.quote ?? ''
  if (!quote) return { quote, body: note.body }
  // Older capture drafts stored the same source excerpt at the start of Markdown.
  // Move only that exact standalone block to the excerpt; storage stays untouched.
  const block = '> ' + quote.replaceAll('\n', '\n> ')
  const body = note.body === block ? ''
    : note.body.startsWith(block + '\n\n') ? note.body.slice(block.length + 2)
      : note.body
  return { quote, body }
}

export function noteCopyText(note: Pick<ReadingNote, 'body' | 'textAnchor' | 'pdfAnchor' | 'subtitleReferences'>): string {
  if (note.subtitleReferences?.length) {
    return [...note.subtitleReferences.map(reference => `> ${reference.text.replaceAll('\n', '\n> ')}\n\n字幕时间：${reference.start}–${reference.end} 秒（${reference.language}）`), notePresentation(note).body].filter(Boolean).join('\n\n')
  }
  const quote = note.textAnchor?.quote ?? note.pdfAnchor?.quote
  if (!quote) return note.body
  const block = '> ' + quote.replaceAll('\n', '\n> ')
  return note.body.includes(quote) || note.body.includes(block) ? note.body : block + '\n\n' + note.body
}
