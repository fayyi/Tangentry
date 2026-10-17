import { expect, test } from 'vitest'
import { articleTranslationParts } from './articleTranslation'

test('article translation splits UTF-8 safely and preserves paragraph mappings', () => {
  const paragraphs = ['ASCII '.repeat(2000), '中文😀'.repeat(3000), ' '.repeat(16000), '最后一段']
  const { pieces, ranges } = articleTranslationParts(paragraphs)
  for (const piece of pieces) {
    expect(Buffer.byteLength(piece)).toBeLessThanOrEqual(7800)
    expect(piece.trim()).not.toBe('')
    expect(piece).not.toMatch(/[\uD800-\uDBFF]$|^[\uDC00-\uDFFF]/)
  }
  for (const index of [0, 1, 3]) expect(pieces.slice(ranges[index].start, ranges[index].end).join('')).toBe(paragraphs[index])
  expect(ranges[2].start).toBe(ranges[2].end)
  expect(pieces[ranges[3].start]).toBe('最后一段')
})

test('whitespace-only chunks do not consume a translated paragraph index', () => {
  const { pieces, ranges } = articleTranslationParts([' '.repeat(7800) + '正文', '下一段'])
  expect(pieces).toEqual(['正文', '下一段'])
  expect(ranges).toEqual([{ start: 0, end: 1 }, { start: 1, end: 2 }])
})

test('excluded code keeps a paragraph slot without sending text or shifting later translations', () => {
  const { pieces, ranges } = articleTranslationParts(['说明文字', null, '代码之后的说明', null])
  expect(pieces).toEqual(['说明文字', '代码之后的说明'])
  expect(ranges).toEqual([{ start: 0, end: 1 }, { start: 1, end: 1 }, { start: 1, end: 2 }, { start: 2, end: 2 }])
  expect(articleTranslationParts([null, null])).toEqual({ pieces: [], ranges: [{ start: 0, end: 0 }, { start: 0, end: 0 }] })
})
