import type { SummaryResult } from './summaryTypes'

export type SummaryButtonState = 'loading' | 'generating' | 'saved' | 'needs-generation' | 'ready'

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
