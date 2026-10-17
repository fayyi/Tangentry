import { expect, test } from 'vitest'
import { shouldRenderInlineSummary, summaryButtonState, summaryNeedsOutlineRegeneration } from './summaryPresentation'
import type { SummaryResult } from './summaryTypes'

const result = (overrides: Partial<SummaryResult> = {}): SummaryResult => ({
  key: 'summary', contentId: 'rss:fixture', source: 'article', baseUrl: '', model: 'fixture',
  totalCalls: 1, completedCalls: 1, complete: true, overview: '概述', points: [{ text: '要点', indices: [0] }],
  outline: [{ title: '章节', level: 0, indices: [0] }], updatedAt: 1, ...overrides,
})

test('summary button state follows the live generation state', () => {
  expect(summaryButtonState(undefined, true, false)).toBe('loading')
  expect(summaryButtonState(result({ complete: false }), false, false)).toBe('ready')
  expect(summaryButtonState(result(), false, true)).toBe('generating')
  expect(summaryButtonState(result(), false, false)).toBe('saved')
})

test('completed summaries without an outline are marked for regeneration', () => {
  expect(summaryNeedsOutlineRegeneration(result({ outline: [] }))).toBe(true)
  expect(summaryNeedsOutlineRegeneration(result())).toBe(false)
  expect(summaryNeedsOutlineRegeneration(result({ complete: false, outline: [] }))).toBe(false)
})

test('RSS summary has one toolbar action and no inline duplicate', () => {
  expect(shouldRenderInlineSummary(true, true)).toBe(false)
  expect(shouldRenderInlineSummary(true, false)).toBe(false)
  expect(shouldRenderInlineSummary(false, false)).toBe(true)
})
