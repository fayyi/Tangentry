import { Button, Select } from '../../components/ui'
import { ChevronDown, ChevronRight, Clipboard, Copy, LoaderCircle, MessageCircle, RotateCcw, Send, Square, Trash2, ListTree, NotebookPen } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { SummaryOutline } from './summaryTypes'
import type { useSummary } from './useSummary'
import { request } from '../../shared/api/client'
import { buildChatContext } from './chatContext'
import { rememberedChatModel, rememberChatModel, selectedChatModel } from './chatPresentation'
import { mergeModelLists, modelsForTask } from './modelPicker'
import './ai.css'

function clock(seconds: number) { const total = Math.max(0, Math.floor(seconds)); const m = Math.floor(total / 60); return `${m}:${String(total % 60).padStart(2, '0')}` }

function OutlineNode({ node, onSeek, onLocateQuote, onLocatePage, citations, depth = 0, expanded, number = '1' }: { node: SummaryOutline; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void; citations: Map<number, { quote: string; page?: number }>; depth?: number; expanded?: boolean; number?: string }) {
  const [open, setOpen] = useState(expanded ?? depth === 0)
  useEffect(() => { if (expanded !== undefined) setOpen(expanded) }, [expanded])
  const children = node.children ?? []
  const citation = node.indices?.map(index => citations.get(index)).find(Boolean)
  return <li className="ai-outline-node" style={{ paddingLeft: depth * 12 }}><div className="ai-outline-row"><Button variant="quiet" type="button" className="ai-outline-toggle" aria-expanded={open} onClick={() => setOpen(value => !value)}>{children.length ? (open ? <ChevronDown size={14}/> : <ChevronRight size={14}/>) : <span className="ai-outline-dot"/>}<span className="ai-outline-number">{number}</span><span>{node.title}</span></Button>{node.start !== undefined && node.end !== undefined ? <Button variant="quiet" type="button" className="ai-outline-time" onClick={() => onSeek?.(node.start!)}>{clock(node.start)}</Button> : node.page ? <Button variant="quiet" type="button" className="ai-outline-time" onClick={() => onLocatePage?.(node.page!)}>第 {node.page} 页</Button> : citation && onLocateQuote ? <Button variant="quiet" type="button" className="ai-outline-time" onClick={() => onLocateQuote(citation.quote)}>定位</Button> : null}</div>{open && children.length > 0 && <ul>{children.map((child, index) => <OutlineNode key={`${child.title}-${index}`} node={child} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage} citations={citations} depth={depth + 1} expanded={expanded} number={`${number}.${index + 1}`}/>)}</ul>}</li>
}

export function AIOutlinePanel({ summary, onSeek, onLocateQuote, onLocatePage }: { summary: ReturnType<typeof useSummary>; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void }) {
  const [expanded, setExpanded] = useState<boolean | undefined>(undefined)
  const outline = summary.inspection?.result.outline ?? []
  if (!summary.inspection?.result.complete) return <p className="ai-insight-empty">生成摘要后，这里会显示可折叠的大纲。</p>
  if (outline.length === 0) return <p className="ai-insight-empty">这份旧摘要没有大纲，请点击上方 AI 摘要重新生成。</p>
  const citations = new Map((summary.inspection.result.citations ?? []).map(value => [value.index, value]))
  return <><div className="ai-outline-actions"><Button variant="quiet" type="button" className="text-button" onClick={() => setExpanded(true)}>全部展开</Button><Button variant="quiet" type="button" className="text-button" onClick={() => setExpanded(false)}>全部折叠</Button></div><ul className="ai-outline-tree">{outline.map((node, index) => <OutlineNode key={`${node.title}-${index}`} node={node} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage} citations={citations} expanded={expanded}/>)}</ul></>
}

type ChatMessage = { role: 'user' | 'assistant'; content: string; model?: string }
export function AIChatPanel({ contentId, context, revision }: { contentId: string; context: string; revision: number }) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [model, setModel] = useState('')
  const [models, setModels] = useState<string[]>([])
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const composer = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { let alive = true; setMessages([]); setModels([]); setModel(''); void Promise.all([request<{ messages: ChatMessage[] }>(`/ai/chat?contentId=${encodeURIComponent(contentId)}`), request<{ enabledModels?: string[]; availableModels?: string[]; chatModel?: string }>('/ai/settings')]).then(([history, settings]) => { if (!alive) return; setMessages(history.messages); const enabled = [...new Set((settings.enabledModels ?? []).filter(Boolean))]; const chatModels = modelsForTask(mergeModelLists(settings.availableModels ?? [], enabled), enabled, 'chat'); setModels(chatModels); setModel(selectedChatModel(settings) || rememberedChatModel(chatModels)) }).catch(() => {}); return () => { alive = false; abort.current?.abort() } }, [contentId])
  const suggestions = ['这个讲了什么', '列出关键观点', '有哪些值得记下的地方']
  const resize = () => { const element = composer.current; if (!element) return; element.style.height = 'auto'; element.style.height = `${Math.min(element.scrollHeight, 180)}px` }
  const send = async (question = input.trim(), conversation = messages) => {
    const value = question.trim(); if (!value || pending || !model) return
    const next = [...conversation, { role: 'user' as const, content: value }]
    setMessages(next); setInput(''); setPending(true); setError('')
    const controller = new AbortController(); abort.current = controller
    try {
      const response = await fetch('/api/ai/chat?stream=1', { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' }, body: JSON.stringify({ contentId, context, messages: next, model, revision }) })
      if (!response.ok || !response.body) { const body = await response.json().catch(() => ({})) as { message?: string }; throw new Error(body.message ?? '对话暂时不可用。') }
      const reader = response.body.getReader(), decoder = new TextDecoder()
      let buffer = '', answer = ''
      setMessages([...next, { role: 'assistant', content: '', model }])
      while (true) {
        const chunk = await reader.read(); if (chunk.done) break
        buffer += decoder.decode(chunk.value, { stream: true })
        const events = buffer.split(/\r?\n\r?\n/); buffer = events.pop() ?? ''
        for (const event of events) {
          const line = event.split('\n').find(value => value.startsWith('data:'))
          if (!line) continue
          const data = JSON.parse(line.slice(5).trim()) as { delta?: string; error?: string; messages?: ChatMessage[] }
          if (data.error) throw new Error(data.error)
          if (data.delta) { answer += data.delta; setMessages([...next, { role: 'assistant', content: answer, model }]) }
        }
      }
      buffer += decoder.decode()
      if (!answer) throw new Error('对话暂时不可用。')
    }
    catch (cause) { if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(cause instanceof Error ? cause.message : '对话暂时不可用。') }
    finally { setPending(false) }
  }
  const clear = async () => { if (pending) return; setMessages([]); try { await request('/ai/chat', { method: 'POST', body: JSON.stringify({ contentId, context, messages: [], revision, clear: true }) }) } catch { /* local state is already clear */ } }
  const copy = async (text: string) => { await navigator.clipboard?.writeText(text) }
  const saveNote = async (text: string) => { try { await request('/notes', { method: 'POST', body: JSON.stringify({ contentId, body: text, tags: ['AI对话'], subtitleReferences: [] }) }) } catch (cause) { setError(cause instanceof Error ? cause.message : '保存笔记失败。') } }
  const regenerate = (index: number) => { const previous = messages[index - 1]; if (previous?.role !== 'user' || pending) return; void send(previous.content, messages.slice(0, index - 1)) }
  return <div className="ai-chat-panel"><div className="ai-chat-actions"><Button variant="quiet" iconOnly type="button" className="icon-button" title="清空对话" aria-label="清空对话" disabled={pending || messages.length === 0} onClick={() => void clear()}><Trash2 size={15}/></Button></div><div className="ai-chat-messages" aria-live="polite">{messages.length === 0 && <div className="ai-chat-empty"><p className="ai-insight-empty">围绕当前内容提问，AI 会结合字幕、正文和摘要回答。</p><div className="ai-chat-suggestions">{suggestions.map(value => <Button variant="quiet" key={value} type="button" disabled={pending || !model} onClick={() => void send(value)}>{value}</Button>)}</div></div>}{messages.map((message, index) => <div key={`${message.role}-${index}`} className={`ai-chat-message ai-chat-${message.role}`}><strong>{message.role === 'user' ? '你' : 'AI'}{message.model && <small>{message.model}</small>}</strong>{message.role === 'assistant' ? <div className="ai-chat-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} components={{ code({ children, className }) { const text = String(children).replace(/\n$/, ''); return <div className="ai-code"><pre className={className}><code>{text}</code></pre><Button variant="quiet" iconOnly type="button" className="icon-button" title="复制代码" aria-label="复制代码" onClick={() => void copy(text)}><Clipboard size={14}/></Button></div> } }}>{message.content}</ReactMarkdown></div> : <p>{message.content}</p>}{message.role === 'assistant' && message.content && !pending && <div className="ai-chat-message-actions"><Button variant="quiet" iconOnly type="button" className="icon-button" title="复制回答" aria-label="复制回答" onClick={() => void copy(message.content)}><Copy size={14}/></Button><Button variant="quiet" iconOnly type="button" className="icon-button" title="重新生成" aria-label="重新生成" onClick={() => regenerate(index)}><RotateCcw size={14}/></Button><Button variant="quiet" iconOnly type="button" className="icon-button" title="存为笔记" aria-label="存为笔记" onClick={() => void saveNote(message.content)}><NotebookPen size={14}/></Button></div>}</div>)}{pending && <p className="ai-chat-pending"><LoaderCircle size={14} className="spin"/>正在生成…</p>}</div>{!model && <p className="ai-chat-setup" role="status">尚未设置对话模型，请前往 AI 设置选择通用模型。</p>}<div className="ai-chat-composer"><textarea ref={composer} value={input} onChange={event => { setInput(event.target.value); resize() }} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void send() } }} placeholder="输入问题" rows={2} disabled={pending}/><div className="ai-chat-toolbar"><Select aria-label="对话模型" value={model} onChange={event => { setModel(event.target.value); rememberChatModel(event.target.value) }} disabled={pending || models.length === 0}><option value="">选择模型</option>{models.map(value => <option key={value} value={value}>{value}</option>)}</Select>{pending ? <Button variant="quiet" iconOnly type="button" className="icon-button" title="停止生成" aria-label="停止生成" onClick={() => abort.current?.abort()}><Square size={16}/></Button> : <Button variant="quiet" iconOnly type="button" className="icon-button" title="发送" aria-label="发送" disabled={!input.trim() || !model} onClick={() => void send()}><Send size={16}/></Button>}</div></div>{error && <p className="ai-summary-feedback" role="alert">{error}</p>}</div>
}

export function AIInsights({ summary, onSeek, onLocateQuote, onLocatePage, contentId, context, revision }: { summary: ReturnType<typeof useSummary>; onSeek?: (seconds: number) => void; onLocateQuote?: (quote: string) => void; onLocatePage?: (page: number) => void; contentId: string; context: string; revision: number }) {
  const [tab, setTab] = useState<'outline' | 'chat'>('outline')
  const hasOutline = Boolean(summary.inspection?.result.outline?.length)
  const summaryContext = useMemo(() => {
    const result = summary.inspection?.result
    return buildChatContext(context, result?.overview, result?.points?.map(point => point.text))
  }, [context, summary.inspection?.result])
  return <section className="ai-insights" aria-label="AI 辅助"><div className="ai-insights-tabs" role="tablist"><Button variant="quiet" type="button" role="tab" aria-selected={tab === 'outline'} onClick={() => setTab('outline')}><ListTree size={15}/>AI 大纲{hasOutline ? ` · ${summary.inspection?.result.outline?.length}` : ''}</Button><Button variant="quiet" type="button" role="tab" aria-selected={tab === 'chat'} onClick={() => setTab('chat')}><MessageCircle size={15}/>AI 对话</Button></div>{tab === 'outline' ? <AIOutlinePanel summary={summary} onSeek={onSeek} onLocateQuote={onLocateQuote} onLocatePage={onLocatePage}/> : <AIChatPanel contentId={contentId} context={summaryContext} revision={revision}/>}</section>
}
