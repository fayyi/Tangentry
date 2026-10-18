import { expect, test } from 'vitest'
import { shouldRenderInlineSummary, summaryAvailability, summaryButtonState, summaryNeedsOutlineRegeneration } from './summaryPresentation'
import { summaryInputFromTrack } from './useSummary'
import type { SummaryResult } from './summaryTypes'
import type { SubtitleTrack } from '../../shared/api/types'

const result = (overrides: Partial<SummaryResult> = {}): SummaryResult => ({
  key: 'summary', contentId: 'rss:fixture', source: 'article', baseUrl: '', model: 'fixture',
  totalCalls: 1, completedCalls: 1, complete: true, overview: '概述', points: [{ text: '要点', indices: [0] }],
  outline: [{ title: '章节', level: 0, indices: [0] }], updatedAt: 1, ...overrides,
})

const inspection = (overrides: Partial<{ ready: boolean; summaryReady: boolean; translating: boolean; active: boolean }> = {}) => ({
  settings: { protocol: 'openai' as const, baseUrl: 'https://ai.example/v1', model: 'fixture', hasKey: true, ready: overrides.ready ?? true, summaryReady: overrides.summaryReady ?? true, translating: overrides.translating ?? false, revision: 1 },
  result: result({ complete: false }), active: overrides.active ?? false,
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

test('summary availability explains every blocked state', () => {
  expect(summaryAvailability({ input: true, inspection: null, loading: true, pending: false })).toBe('loading')
  expect(summaryAvailability({ input: false, inspection: null, loading: false, pending: false })).toBe('missing-input')
  expect(summaryAvailability({ input: true, inspection: inspection({ summaryReady: false }), loading: false, pending: false })).toBe('ai-not-configured')
  expect(summaryAvailability({ input: true, inspection: inspection({ active: true }), loading: false, pending: false })).toBe('busy')
  expect(summaryAvailability({ input: true, inspection: null, loading: false, pending: false, error: '读取失败' })).toBe('error')
  expect(summaryAvailability({ input: true, inspection: inspection({ ready: false, summaryReady: false }), loading: false, pending: false })).toBe('ai-not-configured')
})

test('automatic 167-cue subtitles build summary input without player readiness', () => {
  const track: SubtitleTrack = { language: 'zh-CN', label: '中文', automatic: true, cues: Array.from({ length: 167 }, (_, index) => ({ start: index, end: index + 1, text: `字幕 ${index}` })) }
  const input = summaryInputFromTrack('bilibili:BVfixture1234', 'subtitle', track)
  expect(input.segments).toHaveLength(167)
  expect(input.segments[0]).toEqual({ index: 0, start: 0, end: 1, text: '字幕 0' })
  expect(input.segments[166].index).toBe(166)
  expect(summaryAvailability({ input: true, inspection: inspection(), loading: false, pending: false })).toBe('ready')
})
