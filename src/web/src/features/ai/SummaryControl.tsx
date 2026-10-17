import { LoaderCircle, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
import type { useSummary } from './useSummary'
import type { SummarySource } from './summaryMarkdown'
import { summaryButtonState, summaryNeedsOutlineRegeneration } from './summaryPresentation'
import './ai.css'

export function SummaryControl({ summary, source: _source, onLocateQuote: _onLocateQuote, emptyMessage, emptyAction }: {
  summary: ReturnType<typeof useSummary>; onLocateQuote?: (quote: string) => string | null; source?: SummarySource
  emptyMessage?: string; emptyAction?: ReactNode
}) {
  const { inspection, loading, pending, error, notice, generate, cancel } = summary
  const result = inspection?.result
  const ready = inspection?.settings.summaryReady ?? inspection?.settings.ready
  const state = summaryButtonState(result, loading, pending)
  const disabled = loading || pending || !inspection || !ready || Boolean(inspection?.active) || Boolean(inspection?.settings.translating) || state === 'saved'
  const label = state === 'loading' ? 'AI 摘要 · 读取中' : state === 'generating' ? 'AI 摘要 · 生成中' : state === 'saved' ? 'AI 摘要 · 已保存' : state === 'needs-generation' ? 'AI 摘要 · 补生成大纲' : 'AI 摘要'
  return <div className="ai-summary-control">
    <button type="button" className="ai-inline-trigger" aria-busy={pending || loading} disabled={disabled} title={state === 'needs-generation' ? '旧摘要没有大纲，点击重新生成' : undefined} onClick={() => void generate(summaryNeedsOutlineRegeneration(result))}>{pending ? <LoaderCircle size={14} className="spin" aria-hidden="true" /> : <Sparkles size={14} aria-hidden="true" />}{label}</button>
    {!inspection && !loading && (emptyMessage || emptyAction) && <div className="ai-summary-empty"><p className="ai-summary-muted">{emptyMessage}</p>{emptyAction}</div>}
    {pending && <p className="ai-summary-muted" role="status">正在生成，已完成 {result?.completedCalls ?? 0} / {result?.totalCalls ?? 0} 次 <button type="button" className="text-button" onClick={cancel}>取消</button></p>}
    {notice && <p className="ai-summary-feedback" role="status">{notice}</p>}
    {error && <p className="ai-summary-feedback" role="alert">{error} <button type="button" className="text-button" onClick={() => void summary.reload()}>重新读取摘要状态</button></p>}
  </div>
}
