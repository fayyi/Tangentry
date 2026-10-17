import { ChevronDown, ChevronRight, LoaderCircle, MessageCircle, Send, Trash2, ListTree } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import type { SummaryOutline } from './summaryTypes'
import type { useSummary } from './useSummary'
import { request } from '../../shared/api/client'
import './ai.css'

function clock(seconds: number) { const total = Math.max(0, Math.floor(seconds)); const m = Math.floor(total / 60); return `${m}:${String(total % 60).padStart(2, '0')}` }

function OutlineNode({ node, onSeek, onLocateQuote, onLocatePage, citations, depth = 0, expanded }: { node: SummaryOutline; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void; citations: Map<number, { quote: string; page?: number }>; depth?: number; expanded?: boolean }) {
  const [open, setOpen] = useState(expanded ?? depth === 0)
  useEffect(() => { if (expanded !== undefined) setOpen(expanded) }, [expanded])
  const children = node.children ?? []
  const citation = node.indices?.map(index => citations.get(index)).find(Boolean)
  return <li className="ai-outline-node" style={{ paddingLeft: depth * 12 }}><div className="ai-outline-row"><button type="button" className="ai-outline-toggle" aria-expanded={open} onClick={() => setOpen(value => !value)}>{children.length ? (open ? <ChevronDown size={14}/> : <ChevronRight size={14}/>) : <span className="ai-outline-dot"/>}<span>{node.title}</span></button>{node.start !== undefined && node.end !== undefined ? <button type="button" className="ai-outline-time" onClick={() => onSeek?.(node.start!)}>{clock(node.start)}</button> : node.page ? <button type="button" className="ai-outline-time" onClick={() => onLocatePage?.(node.page!)}>第 {node.page} 页</button> : citation && onLocateQuote ? <button type="button" className="ai-outline-time" onClick={() => onLocateQuote(citation.quote)}>定位</button> : null}</div>{open && children.length > 0 && <ul>{children.map((child, index) => <OutlineNode key={`${child.title}-${index}`} node={child} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage} citations={citations} depth={depth + 1} expanded={expanded}/>)}</ul>}</li>
}

export function AIOutlinePanel({ summary, onSeek, onLocateQuote, onLocatePage }: { summary: ReturnType<typeof useSummary>; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void }) {
  const [expanded, setExpanded] = useState<boolean | undefined>(undefined)
  const outline = summary.inspection?.result.outline ?? []
  if (!summary.inspection?.result.complete) return <p className="ai-insight-empty">生成摘要后，这里会显示可折叠的大纲。</p>
  if (outline.length === 0) return <p className="ai-insight-empty">这份旧摘要没有大纲，请点击上方 AI 摘要重新生成。</p>
  const citations = new Map((summary.inspection.result.citations ?? []).map(value => [value.index, value]))
  return <><div className="ai-outline-actions"><button type="button" className="text-button" onClick={() => setExpanded(true)}>全部展开</button><button type="button" className="text-button" onClick={() => setExpanded(false)}>全部折叠</button></div><ul className="ai-outline-tree">{outline.map((node, index) => <OutlineNode key={`${node.title}-${index}`} node={node} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage} citations={citations} expanded={expanded}/>)}</ul></>
}

export function AIChatPanel({ contentId, context, revision }: { contentId: string; context: string; revision: number }) {
  const [messages, setMessages] = useState<{ role: 'user' | 'assistant'; content: string }[]>([])
  const [input, setInput] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { let alive = true; void request<{ messages: typeof messages }>(`/ai/chat?contentId=${encodeURIComponent(contentId)}`).then(result => { if (alive) setMessages(result.messages) }).catch(() => {}); return () => { alive = false } }, [contentId])
  const send = async () => {
    const value = input.trim(); if (!value || pending) return
    const next = [...messages, { role: 'user' as const, content: value }]
    setMessages(next); setInput(''); setPending(true); setError('')
    try {
      const response = await fetch('/api/ai/chat?stream=1', { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' }, body: JSON.stringify({ contentId, context, messages: next, revision }) })
      if (!response.ok || !response.body) throw new Error('对话暂时不可用。')
      const reader = response.body.getReader(), decoder = new TextDecoder()
      let buffer = '', answer = ''
      setMessages([...next, { role: 'assistant', content: '' }])
      while (true) {
        const chunk = await reader.read(); if (chunk.done) break
        buffer += decoder.decode(chunk.value, { stream: true })
        for (const event of buffer.split('\n\n').slice(0, -1)) {
          buffer = buffer.slice(event.length + 2)
          const line = event.split('\n').find(value => value.startsWith('data:'))
          if (!line) continue
          const data = JSON.parse(line.slice(5).trim()) as { delta?: string; error?: string }
          if (data.error) throw new Error(data.error)
          if (data.delta) { answer += data.delta; setMessages([...next, { role: 'assistant', content: answer }]) }
        }
      }
      buffer += decoder.decode()
      if (!answer) throw new Error('对话暂时不可用。')
    }
    catch (cause) { setError(cause instanceof Error ? cause.message : '对话暂时不可用。') }
    finally { setPending(false) }
  }
  const clear = async () => { if (pending) return; setMessages([]); try { await request('/ai/chat', { method: 'POST', body: JSON.stringify({ contentId, context, messages: [], revision, clear: true }) }) } catch { /* local state is already clear */ } }
  return <div className="ai-chat-panel"><div className="ai-chat-actions"><button type="button" className="icon-button" title="清空对话" aria-label="清空对话" disabled={pending || messages.length === 0} onClick={() => void clear()}><Trash2 size={15}/></button></div><div className="ai-chat-messages" aria-live="polite">{messages.length === 0 && <p className="ai-insight-empty">围绕当前内容提问，AI 会结合字幕、正文和摘要回答。</p>}{messages.map((message, index) => <div key={`${message.role}-${index}`} className={`ai-chat-message ai-chat-${message.role}`}><strong>{message.role === 'user' ? '你' : 'AI'}</strong><p>{message.content}</p></div>)}{pending && <p className="ai-chat-pending"><LoaderCircle size={14} className="spin"/>正在思考…</p>}</div><div className="ai-chat-composer"><textarea value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void send() } }} placeholder="输入问题" rows={2} disabled={pending}/><button type="button" className="icon-button" title="发送" aria-label="发送" disabled={pending || !input.trim()} onClick={() => void send()}><Send size={16}/></button></div>{error && <p className="ai-summary-feedback" role="alert">{error}</p>}</div>
}

export function AIInsights({ summary, onSeek, onLocateQuote, onLocatePage, contentId, context, revision }: { summary: ReturnType<typeof useSummary>; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void; contentId: string; context: string; revision: number }) {
  const [tab, setTab] = useState<'outline' | 'chat'>('outline')
  const hasOutline = Boolean(summary.inspection?.result.outline?.length)
  const summaryContext = useMemo(() => {
    if (context) return context
    const result = summary.inspection?.result
    if (!result) return ''
    return [result.overview, ...(result.points ?? []).map(point => point.text)].filter(Boolean).join('\n')
  }, [context, summary.inspection?.result])
  return <section className="ai-insights" aria-label="AI 辅助"><div className="ai-insights-tabs" role="tablist"><button type="button" role="tab" aria-selected={tab === 'outline'} onClick={() => setTab('outline')}><ListTree size={15}/>AI 大纲{hasOutline ? ` · ${summary.inspection?.result.outline?.length}` : ''}</button><button type="button" role="tab" aria-selected={tab === 'chat'} onClick={() => setTab('chat')}><MessageCircle size={15}/>AI 对话</button></div>{tab === 'outline' ? <AIOutlinePanel summary={summary} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage}/> : <AIChatPanel contentId={contentId} context={summaryContext} revision={revision}/>}</section>
}
