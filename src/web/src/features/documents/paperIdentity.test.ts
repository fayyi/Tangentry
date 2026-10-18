import { expect, test } from 'vitest'
import { isPaperQuery, parsePaperIdentity } from './paperIdentity'

test('recognizes arXiv identifiers, versions, and supported paper URLs', () => {
  for (const value of [
    '2610.22322', 'arXiv:2610.22322v2', 'https://arxiv.org/abs/2610.22322v3',
    'https://arxiv.org/pdf/2610.22322v1.pdf', 'https://huggingface.co/papers/2610.22322',
    'https://alphaxiv.org/abs/2610.22322v2', 'math.AG/0612345',
  ]) expect(isPaperQuery(value), value).toBe(true)
  expect(parsePaperIdentity(' https://arxiv.org/pdf/2610.22322v4.pdf ')).toEqual({ arxivId: '2610.22322', version: 4 })
  expect(parsePaperIdentity('arXiv:2610.22322')).toEqual({ arxivId: '2610.22322' })
})

test('does not classify unrelated or malformed URLs as papers', () => {
  for (const value of ['', 'https://example.com/2610.22322', '2610.223', 'arxiv:2610.22322v0', 'https://arxiv.org/html/2610.22322']) {
    expect(parsePaperIdentity(value), value).toBeNull()
  }
})
