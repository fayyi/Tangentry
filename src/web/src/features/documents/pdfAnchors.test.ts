import { describe, expect, it } from 'vitest'
import { pdfSummarySegments } from './pdfAnchors'

describe('pdfSummarySegments', () => {
  it('creates ordered page anchors and drops empty pages', () => {
    const segments = pdfSummarySegments([{ page: 1, text: ' 第一页 ' }, { page: 2, text: '   ' }, { page: 3, text: '第三页' }])
    expect(segments.map(value => ({ index: value.index, page: value.page, text: value.text }))).toEqual([
      { index: 0, page: 1, text: '第一页' },
      { index: 1, page: 3, text: '第三页' },
    ])
  })
})
