import { expect, test } from 'vitest'
import { noteCopyText, notePresentation } from './notePresentation'
import { tagValidation } from './TagInput'

test('tag validation matches bounded Unicode backend rules', () => {
  for (const value of ['', '#学习, Go, go', '𠀀'.repeat(32), 'İ'.repeat(32), 'abc_def-123', '学习/资料', '#学习/阅读/书摘']) expect(tagValidation(value)).toBe('')
  expect(tagValidation('𠀀'.repeat(33))).toContain('32')
  expect(tagValidation('İ'.repeat(33))).toContain('32')
  for (const value of ['学习//资料', '/学习', '学习/']) expect(tagValidation(value)).toContain('只能包含')
  expect(tagValidation('#')).toContain('只能包含')
  expect(tagValidation(Array(13).fill('相同').join(','))).toContain('12')
})

test('excerpt presentation moves only an exact leading standalone quote and preserves Markdown elsewhere', () => {
  const quote = '第一行 😀\n第二行 `代码`'
  const textAnchor = { quote, start: 0, end: quote.length, prefix: '', suffix: '', sourceHash: 'a'.repeat(64) }
  const block = '> 第一行 😀\n> 第二行 `代码`'
  expect(notePresentation({ body: block, textAnchor })).toEqual({ quote, body: '' })
  expect(notePresentation({ body: block + '\n\n我的思考', textAnchor })).toEqual({ quote, body: '我的思考' })
  for (const body of [
    '```\n' + block + '\n```',
    '开头的想法\n\n' + block,
    block + '\n> 同一引用里还有其他内容',
    block + '多出来的文字\n\n不能丢失',
    '这里提到' + quote + '，但没有引用块。',
  ]) expect(notePresentation({ body, textAnchor }).body).toBe(body)
  expect(notePresentation({ body: block, textAnchor: null, pdfAnchor: null })).toEqual({ quote: '', body: block })
})

test('copy preserves multiline source blocks without duplicating or rewriting saved Markdown', () => {
  const quote = '第一行😀\n第二行 `代码`'
  const anchor = { quote, start: 0, end: quote.length, prefix: '', suffix: '', sourceHash: 'a'.repeat(64) }
  const block = '> 第一行😀\n> 第二行 `代码`'
  for (const body of [block, block + '\n\n自己的想法', '```\n' + block + '\n```', '前文\n\n' + quote]) {
    expect(noteCopyText({ body, textAnchor: anchor })).toBe(body)
  }
  expect(noteCopyText({ body: '只有想法', textAnchor: anchor })).toBe(block + '\n\n只有想法')
  expect(noteCopyText({ body: '**随记**', textAnchor: null })).toBe('**随记**')
})
