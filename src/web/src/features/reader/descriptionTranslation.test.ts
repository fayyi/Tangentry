import { expect, test } from 'vitest'
import { descriptionTranslationParts } from './descriptionTranslation'

test('description sentences retain URLs, titles, line breaks and UTF-8 chunk mappings', () => {
  const text = 'Dr. Smith uses CNC v2.5 parts. Read https://example.com/story.\n\n00:39 First chapter\n' + '中文😀'.repeat(3000) + '。 Next sentence.'
  const { sentences, pieces, ranges } = descriptionTranslationParts(text)
  expect(sentences.join('')).toBe(text)
  expect(sentences.slice(0, 5)).toEqual(['Dr. Smith uses CNC v2.5 parts. ', 'Read https://example.com/story.', '\n\n', '00:39 First chapter', '\n'])
  for (const piece of pieces) {
    expect(Buffer.byteLength(piece)).toBeLessThanOrEqual(7800)
    expect(piece).not.toMatch(/[\uD800-\uDBFF]$|^[\uDC00-\uDFFF]/)
  }
  for (const [index, sentence] of sentences.entries()) {
    const { start, end } = ranges[index]
    expect(pieces.slice(start, end).join('')).toBe(sentence.trim() ? sentence : '')
  }
})
