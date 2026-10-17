import { Copy, Download, FileText, LoaderCircle, Sparkles, X } from 'lucide-react'
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import type { useSummary } from './useSummary'
import { summaryMarkdown, type SummarySource } from './summaryMarkdown'
import { saveFile } from '../../shared/browser/saveFile'
import './ai.css'

function clock(seconds: number) { const total = Math.max(0, Math.floor(seconds)); const h = Math.floor(total / 3600); const m = Math.floor(total / 60) % 60; const s = total % 60; return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}` }

export function SummaryControl({ summary, onLocateQuote, source }: { summary: ReturnType<typeof useSummary>; onLocateQuote?: (quote: string) => string | null; source?: SummarySource }) {
  const { inspection, loading, pending, error, notice, open, setOpen, generate, cancel, onSeek } = summary
  const result = inspection?.result; const done = result?.complete; const points = result?.points ?? []; const citations = new Map((result?.citations ?? []).map(value => [value.index, value]))
  const ready = inspection?.settings.summaryReady ?? inspection?.settings.ready
  const id = useId()
  const root = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null)
  const previousOpen = useRef(false)
  const [citationNotice, setCitationNotice] = useState('')
  const [copying, setCopying] = useState(false), [exportNotice, setExportNotice] = useState(''), [exportError, setExportError] = useState('')
  const [exportNeedsRead, setExportNeedsRead] = useState(false)
  const copyAttempt = useRef(0)
  useEffect(() => {
    setCopying(false); setExportNotice(''); setExportError(''); setExportNeedsRead(false)
    return () => { copyAttempt.current++ }
  }, [open, result?.key])
  function markdown() {
    if (!result) throw new Error('摘要尚未完成。')
    const context = source ?? { contentId: summary.contentId }
    const reader = new URL('/reader', location.origin)
    reader.searchParams.set('contentId', context.contentId)
    return summaryMarkdown(result, context, reader.href)
  }
  function prepareMarkdown() {
    setExportNotice(''); setExportError(''); setExportNeedsRead(false)
    try { return markdown() }
    catch (cause) {
      setExportError(cause instanceof Error ? cause.message : '摘要无法导出，请重新读取。')
      setExportNeedsRead(true)
      return null
    }
  }
  async function copy() {
    if (copying) return
    const attempt = ++copyAttempt.current
    setCopying(true)
    const value = prepareMarkdown()
    if (value === null) {
      setCopying(false)
      return
    }
    try {
      await navigator.clipboard.writeText(value)
      if (attempt === copyAttempt.current) setExportNotice('摘要已复制。')
    } catch {
      if (attempt === copyAttempt.current) setExportError('无法复制，请导出文件。')
    } finally { if (attempt === copyAttempt.current) setCopying(false) }
  }
  function download() {
    const value = prepareMarkdown()
    if (value === null) return
    try {
      saveFile(value, `Tangentry-summary-${result!.key.replace(/[^a-zA-Z0-9_-]/g, '').slice(0, 24) || 'content'}.md`, 'text/markdown;charset=utf-8')
      setExportNotice('摘要已导出。')
    } catch { setExportError('导出失败，请重试。') }
  }
  useEffect(() => { setCitationNotice('') }, [open, result?.key])
  useLayoutEffect(() => {
    if (!open) return
    const position = () => {
      const anchor = trigger.current?.getBoundingClientRect(), node = panel.current
      if (!anchor || !node) return
      const workspace = root.current?.closest('main')?.getBoundingClientRect()
      const margin = 16, start = Math.max(0, workspace?.left ?? 0), end = Math.min(innerWidth, workspace?.right ?? innerWidth)
      const width = Math.min(420, end - start - margin * 2)
      const left = Math.max(start + margin, Math.min(end - width - margin, anchor.right - width))
      const top = Math.max(76, Math.min(anchor.bottom + 8, innerHeight - 160))
      node.style.position = 'fixed'
      node.style.width = `${width}px`
      node.style.left = `${left}px`
      node.style.right = 'auto'
      node.style.top = `${top}px`
      node.style.maxHeight = `${Math.max(80, innerHeight - top - margin)}px`
    }
    position()
    window.addEventListener('resize', position)
    window.addEventListener('scroll', position, true)
    return () => { window.removeEventListener('resize', position); window.removeEventListener('scroll', position, true) }
  }, [open])
  function close() {
    setOpen(false)
    trigger.current?.focus({ preventScroll: true })
  }
  useLayoutEffect(() => {
    if (open && (!previousOpen.current || document.activeElement === document.body)) panel.current?.focus({ preventScroll: true })
    previousOpen.current = open
  }, [open, pending, loading, done, error])
  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const focusOutside = (event: FocusEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || !root.current?.contains(document.activeElement)) return
      event.preventDefault()
      close()
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('focusin', focusOutside)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('focusin', focusOutside)
      document.removeEventListener('keydown', escape)
    }
  }, [open, setOpen])
  return <div ref={root} className="ai-summary-control">
    <button ref={trigger} className="ai-inline-trigger" aria-expanded={open} aria-haspopup="dialog" aria-controls={open ? id : undefined} onClick={() => setOpen(value => !value)}><Sparkles size={14} aria-hidden="true" />AI 摘要{done ? ' · 已保存' : ''}</button>
    {open && <div ref={panel} id={id} tabIndex={-1} className="ai-summary-popover" role="dialog" aria-label="AI 摘要">
      <div className="ai-summary-head"><strong><FileText size={15} aria-hidden="true" />AI 摘要</strong><div className="ai-summary-actions">{done && <><button type="button" className="icon-button" aria-label="复制摘要" title="复制摘要" disabled={copying || pending || loading || Boolean(error)} onClick={() => void copy()}>{copying ? <LoaderCircle size={15} className="spin"/> : <Copy size={15}/>}</button><button type="button" className="icon-button" aria-label="导出摘要" title="导出摘要" disabled={pending || loading || Boolean(error)} onClick={download}><Download size={15}/></button></>}<button className="icon-button" aria-label="关闭 AI 摘要" title="关闭 AI 摘要" onClick={close}><X size={15}/></button></div></div>
      {loading && !inspection && <p role="status">正在读取本机摘要…</p>}
      {inspection && !done && !pending && <>{ready ? <p className="ai-summary-muted">预计调用 {result?.totalCalls ?? 0} 次，使用 {result?.model || inspection.settings.model || '未选择模型'}。</p> : <p className="ai-summary-muted">请先在<a href="/tools#translation">翻译与摘要设置</a>配置支持摘要的通用模型。</p>}{!error && <button className="button button-secondary" title={inspection.settings.translating ? 'AI 正在处理其他任务' : undefined} disabled={loading || !ready || inspection.active || inspection.settings.translating} onClick={() => void generate()}>{result?.completedCalls ? '继续生成摘要' : '生成摘要'}</button>}</>}
      {pending && <p role="status">正在生成，已完成 {result?.completedCalls ?? 0} / {result?.totalCalls ?? 0} 次 <button className="text-button" onClick={cancel}>取消</button></p>}
      {done && <div className="ai-summary-result"><p className="ai-summary-overview">{result?.overview}</p><ol>{points.map((point, index) => <li key={`${index}-${point.text}`}><span>{point.text}</span><div className="ai-summary-citations">{point.indices.map(sourceIndex => { const citation = citations.get(sourceIndex); return citation ? citation.start !== undefined && citation.end !== undefined ? <button key={sourceIndex} className="ai-summary-time" onClick={() => onSeek?.(citation.start!)}>{clock(citation.start)}</button> : onLocateQuote ? <button key={sourceIndex} className="ai-summary-quote ai-summary-quote-link" title="定位原文" aria-label={`定位原文：${citation.quote}`} onClick={() => {
        const message = onLocateQuote(citation.quote)
        setCitationNotice(message ?? '')
        if (!message) setOpen(false)
      }}>“{citation.quote.slice(0, 90)}{citation.quote.length > 90 ? '…' : ''}”</button> : <span key={sourceIndex} className="ai-summary-quote">“{citation.quote.slice(0, 90)}{citation.quote.length > 90 ? '…' : ''}”</span> : null })}</div></li>)}</ol></div>}
      {done && !pending && ready && <p className="ai-summary-muted">由 {result?.model} 生成，更换模型后仍会使用这份摘要。<button className="text-button" disabled={loading || inspection?.active || inspection?.settings.translating} onClick={() => void generate(true)}>用当前模型重新生成</button></p>}
      {citationNotice && <p className="ai-summary-feedback" role="status">{citationNotice}</p>}
      {exportNotice && <p className="ai-summary-feedback" role="status">{exportNotice}</p>}
      {exportError && <p className="ai-summary-feedback" role="alert">{exportError}</p>}
      {(!inspection || error || exportNeedsRead) && !pending && <button className="text-button" disabled={loading} onClick={() => { setExportNeedsRead(false); setExportError(''); void summary.reload() }}>重新读取摘要状态</button>}
      {notice && <p className="ai-summary-feedback" role="status">{notice}</p>}{error && <p className="ai-summary-feedback" role="alert">{error}</p>}
    </div>}
  </div>
}
