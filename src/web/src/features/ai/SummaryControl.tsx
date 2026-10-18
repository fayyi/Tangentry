import { LoaderCircle, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
import type { useSummary } from './useSummary'
import type { SummarySource } from './summaryMarkdown'
import { summaryAvailability, summaryControlPresentation, summaryNeedsOutlineRegeneration } from './summaryPresentation'
import './ai.css'

export function openTranscription() {
  const toggle = document.querySelector<HTMLButtonElement>('[data-subtitle-settings-toggle]')
  const needsPanel = toggle?.getAttribute('aria-expanded') !== 'true'
  if (needsPanel) toggle?.click()
  const focusControl = () => {
    const management = document.querySelector<HTMLDetailsElement>('[data-subtitle-management]')
    if (management) management.open = true
    const control = document.querySelector<HTMLElement>('[data-transcription-control]')
    if (!control) {
      document.querySelector<HTMLElement>('#subtitle-title')?.scrollIntoView({ block: 'center', behavior: 'smooth' })
      return
    }
    control.scrollIntoView({ block: 'center', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    const button = control.querySelector<HTMLElement>('button:not([disabled])')
    if (button) button.focus({ preventScroll: true })
    else { control.tabIndex = -1; control.focus({ preventScroll: true }) }
  }
  if (needsPanel) window.setTimeout(focusControl, 0)
  else focusControl()
}

export function SummaryControl({ summary, source: _source, onLocateQuote: _onLocateQuote, emptyMessage, emptyAction }: {
  summary: ReturnType<typeof useSummary>; onLocateQuote?: (quote: string) => string | null; source?: SummarySource
  emptyMessage?: string; emptyAction?: ReactNode
}) {
  const { inspection, loading, pending, error, notice, generate, cancel, reload, hasInput } = summary
  const result = inspection?.result
  const availability = summaryAvailability({ input: hasInput, inspection, loading, pending, error })
  const presentation = summaryControlPresentation(availability)
  const retry = () => { if (availability === 'error' && inspection) void generate(summaryNeedsOutlineRegeneration(result)); else if (availability === 'error') void reload() }
  return <div className="ai-summary-control">
    {presentation.action === 'settings' ? <a className="ai-inline-trigger" href="/tools#translation" title={presentation.title}><Sparkles size={14} aria-hidden="true" />{presentation.label}</a> : <button type="button" className="ai-inline-trigger" aria-busy={pending || loading} disabled={presentation.disabled} title={presentation.title} onClick={availability === 'error' ? retry : () => void generate(summaryNeedsOutlineRegeneration(result))}>{pending ? <LoaderCircle size={14} className="spin" aria-hidden="true" /> : <Sparkles size={14} aria-hidden="true" />}{presentation.label}</button>}
    {presentation.action === 'transcribe' && <div className="ai-summary-empty"><p className="ai-summary-muted">{emptyMessage || '当前没有字幕或可用文字。'}</p>{emptyAction || <button type="button" className="text-button" onClick={openTranscription}>先转录再摘要</button>}</div>}
    {availability === 'busy' && <p className="ai-summary-muted" role="status">另一个 AI 任务正在运行，完成后会自动恢复。请稍候。</p>}
    {pending && <p className="ai-summary-muted" role="status">正在生成，已完成 {result?.completedCalls ?? 0} / {result?.totalCalls ?? 0} 次 <button type="button" className="text-button" onClick={cancel}>取消</button></p>}
    {notice && <p className="ai-summary-feedback" role="status">{notice}</p>}
    {error && <p className="ai-summary-feedback" role="alert">{error} <button type="button" className="text-button" onClick={retry}>{inspection ? '重试生成' : '重新读取摘要状态'}</button></p>}
  </div>
}
