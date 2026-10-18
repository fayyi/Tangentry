import type { SummaryInspection, SummaryResult } from './summaryTypes'
import type { SubtitleTrack } from '../../shared/api/types'

export type SummaryButtonState = 'loading' | 'generating' | 'saved' | 'needs-generation' | 'ready'
export type SummaryAvailability = SummaryButtonState | 'missing-input' | 'ai-not-configured' | 'busy' | 'error'
export type SummaryControlAction = 'none' | 'generate' | 'retry' | 'settings' | 'transcribe'

export function summaryTextFallback(...texts: Array<string | undefined>): string | undefined {
  return texts.find(text => Boolean(text?.trim()))?.trim()
}

export function summaryGenerationBlockedReason({ hasInput, hasInspection, scopeCurrent, loading, pending, mutationPending, active, translating, ready }: {
  hasInput: boolean
  hasInspection: boolean
  scopeCurrent: boolean
  loading: boolean
  pending: boolean
  mutationPending: boolean
  active: boolean
  translating: boolean
  ready: boolean
}): string | undefined {
  if (!hasInput) return '当前没有可用的摘要输入，请先读取字幕或转录。'
  if (!scopeCurrent) return '摘要输入已更新，正在读取新的摘要状态，请稍候再试。'
  if (loading) return '正在读取摘要状态，请稍候再试。'
  if (!hasInspection) return '尚未读取到摘要状态，请点击重试。'
  if (pending || mutationPending) return '摘要请求已在处理中，请稍候。'
  if (active || translating) return '另一个 AI 任务正在运行，请等待完成后重试。'
  if (!ready) return '尚未配置可用的摘要模型，请去设置 AI 选择摘要模型。'
  return undefined
}

export function summaryControlPresentation(availability: SummaryAvailability): { label: string; title?: string; disabled: boolean; action: SummaryControlAction } {
  switch (availability) {
    case 'loading': return { label: 'AI 摘要 · 读取中', title: '正在读取摘要状态，请稍候', disabled: true, action: 'none' }
    case 'generating': return { label: 'AI 摘要 · 生成中', title: '摘要正在生成，请稍候', disabled: true, action: 'none' }
    case 'saved': return { label: 'AI 摘要 · 已保存', title: '这份摘要已经保存', disabled: true, action: 'none' }
    case 'needs-generation': return { label: 'AI 摘要 · 补生成大纲', title: '旧摘要没有大纲，点击重新生成', disabled: false, action: 'generate' }
    case 'ready': return { label: 'AI 摘要', disabled: false, action: 'generate' }
    case 'missing-input': return { label: 'AI 摘要 · 需先转录', title: '当前没有字幕或可用文字，请先转录再摘要', disabled: true, action: 'transcribe' }
    case 'ai-not-configured': return { label: 'AI 摘要 · 去设置', title: '请去设置 AI 选择摘要模型', disabled: false, action: 'settings' }
    case 'busy': return { label: 'AI 摘要 · 其他任务占用', title: '另一个 AI 任务正在运行，请等待完成', disabled: true, action: 'none' }
    case 'error': return { label: 'AI 摘要 · 重试', title: '摘要状态读取失败，点击重试', disabled: false, action: 'retry' }
  }
}

export function summaryControlState(input: Omit<Parameters<typeof summaryAvailability>[0], 'input'> & { input?: boolean; track?: Pick<SubtitleTrack, 'cues'> | null; description?: string }): { availability: SummaryAvailability; label: string; title?: string; disabled: boolean; action: SummaryControlAction } {
  const hasInput = input.input ?? Boolean(input.track?.cues.length || input.description?.trim())
  const availability = summaryAvailability({ ...input, input: hasInput })
  return { availability, ...summaryControlPresentation(availability) }
}

export function summaryAvailability({ input, inspection, loading, pending, error }: {
  input: boolean
  inspection: SummaryInspection | null | undefined
  loading: boolean
  pending: boolean
  error?: string
}): SummaryAvailability {
  if (loading) return 'loading'
  if (pending) return 'generating'
  if (!input) return 'missing-input'
  if (!inspection && !error) return 'loading'
  if (error) return 'error'
  if (!inspection) return 'error'
  if (!(inspection.settings.summaryReady ?? inspection.settings.ready)) return 'ai-not-configured'
  if (inspection.active || inspection.settings.translating) return 'busy'
  if (inspection.result.complete && inspection.result.outline?.length) return 'saved'
  if (inspection.result.complete && !inspection.result.outline?.length) return 'needs-generation'
  return 'ready'
}

export function summaryButtonState(result: SummaryResult | undefined, loading: boolean, pending: boolean): SummaryButtonState {
  if (loading) return 'loading'
  if (pending) return 'generating'
  if (result?.complete && result.outline?.length) return 'saved'
  if (result?.complete && !result.outline?.length) return 'needs-generation'
  if (result) return 'ready'
  return 'loading'
}

export function summaryNeedsOutlineRegeneration(result: SummaryResult | undefined): boolean {
  return Boolean(result?.complete && !result.outline?.length)
}

export function shouldRenderInlineSummary(feed: boolean, toolbarOwnsSummary: boolean): boolean {
  return !feed && !toolbarOwnsSummary
}
