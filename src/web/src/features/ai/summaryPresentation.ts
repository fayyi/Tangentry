import type { SummaryInspection, SummaryResult } from './summaryTypes'

export type SummaryButtonState = 'loading' | 'generating' | 'saved' | 'needs-generation' | 'ready'
export type SummaryAvailability = SummaryButtonState | 'missing-input' | 'ai-not-configured' | 'busy' | 'error'

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
