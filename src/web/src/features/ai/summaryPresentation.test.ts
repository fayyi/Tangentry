import { expect, test } from 'vitest'
import { shouldRenderInlineSummary, summaryAvailability, summaryButtonState, summaryControlPresentation, summaryControlState, summaryEmptyInputPresentation, summaryGenerationBlockedReason, summaryNeedsOutlineRegeneration, summaryStatusPresentation, summaryTextFallback } from './summaryPresentation'
import { summaryInputFromText, summaryInputFromTrack } from './useSummary'
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
  expect(summaryAvailability({ input: false, inspection: null, loading: true, pending: false })).toBe('missing-input')
  expect(summaryAvailability({ input: true, inspection: inspection(), loading: false, pending: true })).toBe('generating')
  expect(summaryAvailability({ input: false, inspection: null, loading: false, pending: false })).toBe('missing-input')
  expect(summaryAvailability({ input: true, inspection: inspection({ summaryReady: false }), loading: false, pending: false })).toBe('ai-not-configured')
  expect(summaryAvailability({ input: true, inspection: inspection({ active: true }), loading: false, pending: false })).toBe('busy')
  expect(summaryAvailability({ input: true, inspection: null, loading: false, pending: false, error: '读取失败' })).toBe('error')
  expect(summaryAvailability({ input: true, inspection: inspection({ ready: false, summaryReady: false }), loading: false, pending: false })).toBe('ai-not-configured')
  expect(summaryAvailability({ input: true, inspection: inspection({ active: false }), loading: false, pending: false })).toBe('ready')
  expect(summaryAvailability({ input: true, inspection: { ...inspection(), result: result() }, loading: false, pending: false })).toBe('saved')
  expect(summaryAvailability({ input: true, inspection: { ...inspection(), result: result({ outline: [] }) }, loading: false, pending: false })).toBe('needs-generation')
})

test('summary control presentation exposes next steps for every state', () => {
  expect(summaryControlPresentation('loading')).toMatchObject({ label: 'AI 摘要 · 读取中', title: '正在读取摘要状态，请稍候', disabled: true, action: 'none' })
  expect(summaryControlPresentation('generating')).toMatchObject({ label: 'AI 摘要 · 生成中', title: '摘要正在生成，请稍候', disabled: true, action: 'none' })
  expect(summaryControlPresentation('saved')).toMatchObject({ label: 'AI 摘要 · 已保存', disabled: true, action: 'none' })
  expect(summaryControlPresentation('needs-generation')).toMatchObject({ disabled: false, action: 'generate' })
  expect(summaryControlPresentation('ready')).toMatchObject({ label: 'AI 摘要', disabled: false, action: 'generate' })
  expect(summaryControlPresentation('ai-not-configured')).toMatchObject({ label: 'AI 摘要 · 去设置', disabled: false, action: 'settings' })
  expect(summaryControlPresentation('error')).toMatchObject({ label: 'AI 摘要 · 重试', disabled: false, action: 'retry' })
  expect(summaryControlPresentation('busy')).toMatchObject({ label: 'AI 摘要 · 其他任务占用', disabled: true, action: 'none' })
  expect(summaryControlPresentation('missing-input')).toMatchObject({ label: 'AI 摘要 · 需先转录', disabled: false, action: 'transcribe' })
})

test('summary status presentation keeps next steps out of the toolbar flow', () => {
  expect(summaryStatusPresentation('missing-input')).toEqual({ message: '当前没有字幕或可用文字。', tone: 'warning', actionLabel: '先转录再摘要' })
  expect(summaryStatusPresentation('saved')).toEqual({ message: '这份摘要已经保存。', tone: 'success' })
  expect(summaryStatusPresentation('ready')).toEqual({ message: '当前内容可以生成摘要。', tone: 'neutral', actionLabel: '生成摘要' })
})

test('PDF empty summary input has a clear explanation without a transcription action', () => {
  expect(summaryEmptyInputPresentation('pdf')).toEqual({ message: '此 PDF 没有可提取的文字，暂时无法摘要。' })
  expect(summaryEmptyInputPresentation('media')).toEqual({ message: '当前没有字幕或可用文字。', actionLabel: '先转录再摘要' })
})

test('summary control state maps content and inspection to the toolbar action', () => {
  const busyBase = { inspection: null, loading: false, pending: false }
  const automaticTrack: SubtitleTrack = { language: 'zh-CN', label: '中文', automatic: true, cues: Array.from({ length: 167 }, (_, index) => ({ start: index, end: index + 1, text: `字幕 ${index}` })) }
  const automaticSubtitleState = summaryControlState({ ...busyBase, track: automaticTrack, inspection: inspection() })
  expect(automaticSubtitleState).toMatchObject({ availability: 'ready', label: 'AI 摘要', disabled: false, action: 'generate' })
  // Summary availability does not depend on playback readiness or player fallback mode.
  const playbackReady = false
  expect(playbackReady).toBe(false)
  expect(automaticSubtitleState.disabled).toBe(false)
  expect(summaryControlState({ ...busyBase, description: '视频简介', inspection: inspection() })).toMatchObject({ availability: 'ready', action: 'generate' })
  expect(summaryControlState({ ...busyBase, track: { cues: [] }, description: '  ' })).toMatchObject({ availability: 'missing-input', label: 'AI 摘要 · 需先转录', action: 'transcribe' })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, loading: true })).toMatchObject({ availability: 'loading', label: 'AI 摘要 · 读取中', disabled: true, action: 'none' })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: inspection(), pending: true })).toMatchObject({ availability: 'generating', label: 'AI 摘要 · 生成中', disabled: true, action: 'none' })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: inspection({ summaryReady: false }) })).toMatchObject({ availability: 'ai-not-configured', label: 'AI 摘要 · 去设置', disabled: false, action: 'settings' })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: inspection({ active: true }) })).toMatchObject({ availability: 'busy', label: 'AI 摘要 · 其他任务占用', disabled: true })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: null, error: '状态读取失败' })).toMatchObject({ availability: 'error', label: 'AI 摘要 · 重试', disabled: false, action: 'retry' })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: { ...inspection(), result: result() } })).toMatchObject({ availability: 'saved', label: 'AI 摘要 · 已保存', disabled: true })
  expect(summaryControlState({ ...busyBase, track: automaticTrack, inspection: { ...inspection(), result: result({ outline: [] }) } })).toMatchObject({ availability: 'needs-generation', label: 'AI 摘要 · 补生成大纲', disabled: false, action: 'generate' })
})

test('summary generation no-op guards report actionable feedback', () => {
  const ready = { hasInput: true, hasInspection: true, scopeCurrent: true, loading: false, pending: false, mutationPending: false, active: false, translating: false, ready: true }
  expect(summaryGenerationBlockedReason({ ...ready, hasInput: false })).toContain('先读取字幕或转录')
  expect(summaryGenerationBlockedReason({ ...ready, scopeCurrent: false })).toContain('摘要输入已更新')
  expect(summaryGenerationBlockedReason({ ...ready, loading: true })).toContain('正在读取摘要状态')
  expect(summaryGenerationBlockedReason({ ...ready, hasInspection: false })).toContain('点击重试')
  expect(summaryGenerationBlockedReason({ ...ready, pending: true })).toContain('处理中')
  expect(summaryGenerationBlockedReason({ ...ready, mutationPending: true })).toContain('处理中')
  expect(summaryGenerationBlockedReason({ ...ready, active: true })).toContain('另一个 AI 任务')
  expect(summaryGenerationBlockedReason({ ...ready, translating: true })).toContain('另一个 AI 任务')
  expect(summaryGenerationBlockedReason({ ...ready, ready: false })).toContain('去设置 AI')
  expect(summaryGenerationBlockedReason(ready)).toBeUndefined()
})

test('automatic 167-cue subtitles build summary input without player readiness', () => {
  const track: SubtitleTrack = { language: 'zh-CN', label: '中文', automatic: true, cues: Array.from({ length: 167 }, (_, index) => ({ start: index, end: index + 1, text: `字幕 ${index}` })) }
  const input = summaryInputFromTrack('bilibili:BVfixture1234', 'subtitle', track)
  expect(input).not.toBeNull()
  expect(input!.segments).toHaveLength(167)
  expect(input!.segments![0]).toEqual({ index: 0, start: 0, end: 1, text: '字幕 0' })
  expect(input!.segments![166].index).toBe(166)
  // Playback readiness is intentionally absent from the summary input.
  expect(summaryAvailability({ input: true, inspection: inspection(), loading: false, pending: false })).toBe('ready')
})

test('empty subtitle tracks are treated as missing input', () => {
  const track: SubtitleTrack = { language: 'zh-CN', label: '中文', automatic: true, cues: [] }
  expect(summaryInputFromTrack('bilibili:BVfixture1234', 'subtitle', track)).toBeNull()
  expect(summaryAvailability({ input: false, inspection: null, loading: false, pending: false })).toBe('missing-input')
})

test('empty subtitle tracks can fall back to available body text', () => {
  const track: SubtitleTrack = { language: 'zh-CN', label: '中文', automatic: true, cues: [] }
  expect(summaryInputFromTrack('bilibili:BVfixture1234', 'subtitle', track)).toBeNull()
  expect(summaryInputFromText('bilibili:BVfixture1234', 'subtitle', '视频简介')).toEqual({ contentId: 'bilibili:BVfixture1234', source: 'subtitle', text: '视频简介', revision: 0 })
  expect(summaryAvailability({ input: true, inspection: inspection(), loading: false, pending: false })).toBe('ready')
})

test('a video description is valid summary input without subtitles', () => {
  const description = '这是视频简介，可作为摘要输入。'
  expect(summaryInputFromText('bilibili:BVfixture1234', 'subtitle', description)).toEqual({ contentId: 'bilibili:BVfixture1234', source: 'subtitle', text: description, revision: 0 })
  expect(summaryAvailability({ input: Boolean(description.trim()), inspection: inspection(), loading: false, pending: false })).toBe('ready')
})

test('direct Bilibili reading uses asynchronously loaded description as summary fallback', () => {
  expect(summaryTextFallback(undefined, '异步读取的视频简介')).toBe('异步读取的视频简介')
  expect(summaryTextFallback('  ', '异步读取的视频简介')).toBe('异步读取的视频简介')
  expect(summaryTextFallback('缓存中的简介', '异步读取的视频简介')).toBe('缓存中的简介')
})
