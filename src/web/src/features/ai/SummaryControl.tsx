import { LoaderCircle, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
import type { useSummary } from './useSummary'
import type { SummarySource } from './summaryMarkdown'
import { summaryAvailability, summaryNeedsOutlineRegeneration } from './summaryPresentation'
import './ai.css'

export function SummaryControl({ summary, source: _source, onLocateQuote: _onLocateQuote, emptyMessage, emptyAction }: {
  summary: ReturnType<typeof useSummary>; onLocateQuote?: (quote: string) => string | null; source?: SummarySource
  emptyMessage?: string; emptyAction?: ReactNode
}) {
  const { inspection, loading, pending, error, notice, generate, cancel, reload, hasInput } = summary
  const result = inspection?.result
  const availability = summaryAvailability({ input: hasInput, inspection, loading, pending, error })
  const disabled = availability === 'loading' || availability === 'generating' || availability === 'missing-input' || availability === 'busy' || availability === 'saved'
  const label = availability === 'loading' ? 'AI 摘要 · 读取中' : availability === 'generating' ? 'AI 摘要 · 生成中' : availability === 'saved' ? 'AI 摘要 · 已保存' : availability === 'needs-generation' ? 'AI 摘要 · 补生成大纲' : availability === 'ai-not-configured' ? 'AI 摘要 · 去设置' : availability === 'missing-input' ? 'AI 摘要 · 需先转录' : availability === 'busy' ? 'AI 摘要 · 其他任务占用' : availability === 'error' ? 'AI 摘要 · 重试' : 'AI 摘要'
  const title = availability === 'missing-input' ? '当前没有字幕或可用文字，请先转录再摘要' : availability === 'ai-not-configured' ? '请去设置 AI 选择摘要模型' : availability === 'busy' ? '另一个 AI 任务正在运行，请等待完成' : availability === 'saved' ? '这份摘要已经保存' : availability === 'needs-generation' ? '旧摘要没有大纲，点击重新生成' : availability === 'error' ? '摘要状态读取失败，点击重试' : undefined
  const retry = () => { if (availability === 'error' && inspection) void generate(summaryNeedsOutlineRegeneration(result)); else if (availability === 'error') void reload() }
  return <div className="ai-summary-control">
    {availability === 'ai-not-configured' ? <a className="ai-inline-trigger" href="/tools#translation" title={title}><Sparkles size={14} aria-hidden="true" />{label}</a> : <button type="button" className="ai-inline-trigger" aria-busy={pending || loading} disabled={disabled} title={title} onClick={availability === 'error' ? retry : () => void generate(summaryNeedsOutlineRegeneration(result))}>{pending ? <LoaderCircle size={14} className="spin" aria-hidden="true" /> : <Sparkles size={14} aria-hidden="true" />}{label}</button>}
    {availability === 'missing-input' && <div className="ai-summary-empty"><p className="ai-summary-muted">{emptyMessage || '当前没有字幕或可用文字。'}</p>{emptyAction || <a className="text-button" href="#subtitle-title">先转录再摘要</a>}</div>}
    {availability === 'busy' && <p className="ai-summary-muted" role="status">另一个 AI 任务正在运行，完成后会自动恢复。请稍候。</p>}
    {pending && <p className="ai-summary-muted" role="status">正在生成，已完成 {result?.completedCalls ?? 0} / {result?.totalCalls ?? 0} 次 <button type="button" className="text-button" onClick={cancel}>取消</button></p>}
    {notice && <p className="ai-summary-feedback" role="status">{notice}</p>}
    {error && <p className="ai-summary-feedback" role="alert">{error} <button type="button" className="text-button" onClick={retry}>{inspection ? '重试生成' : '重新读取摘要状态'}</button></p>}
  </div>
}
