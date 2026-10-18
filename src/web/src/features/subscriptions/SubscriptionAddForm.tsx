import { AlertCircle, Check, Link2, LoaderCircle, Plus, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import { request } from '../../shared/api/client'

type ResolvedSource = { type: string; query: string; title: string; url: string }
const sourceLabels: Record<string, string> = { bilibili: 'B站 UP 主', youtube: 'YouTube 频道', rss: 'RSS / 播客', arxiv: 'arXiv 分类', huggingface: 'HF 每日论文' }
const commonArxivCategories = ['cs.AI', 'cs.CL', 'cs.LG', 'cs.CV']

export function SubscriptionAddForm({ busy, creating, inputRef, onCreate }: { busy: boolean; creating: boolean; inputRef: RefObject<HTMLInputElement | null>; onCreate: (query: string, name: string, type: string) => Promise<boolean> }) {
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState<'default' | 'arxiv' | 'huggingface'>('default')
  const [name, setName] = useState('')
  const [resolved, setResolved] = useState<ResolvedSource | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const editedName = useRef(false)
  const controller = useRef<AbortController | null>(null)
  const timer = useRef<number | undefined>(undefined)
  const generation = useRef(0)
  const submitting = useRef(false)
  function cancel() { window.clearTimeout(timer.current); controller.current?.abort(); controller.current = null; generation.current++; setLoading(false) }
  useEffect(() => () => { window.clearTimeout(timer.current); controller.current?.abort() }, [])
  async function resolve(value: string) {
    const typedValue = kind === 'arxiv' ? `arxiv:${value.trim()}` : kind === 'huggingface' ? `huggingface:${value.trim() || 'today'}` : value.trim()
    cancel(); const token = generation.current; const active = new AbortController(); controller.current = active; setLoading(true); setError('')
    try { const result = await request<ResolvedSource>('/subscriptions/resolve', { method: 'POST', body: JSON.stringify({ query: typedValue }), signal: active.signal }); if (!active.signal.aborted && token === generation.current) { setResolved(result); if (!editedName.current) setName(result.title) }; return result }
    catch (cause) { if (!active.signal.aborted && token === generation.current) setError(cause instanceof Error ? cause.message : '暂时无法识别这个订阅地址。'); return null }
    finally { if (!active.signal.aborted && token === generation.current) { setLoading(false); controller.current = null } }
  }
  function changeQuery(value: string) { cancel(); setQuery(value); setResolved(null); setError(''); if (!editedName.current) setName(''); if (value.trim()) timer.current = window.setTimeout(() => { void resolve(value) }, 650) }
  async function submit() { if (busy || loading || submitting.current || kind === 'default' && !query.trim()) return; submitting.current = true; try { const source = resolved ?? await resolve(query); if (!source) return; if (await onCreate(source.query, editedName.current ? name.trim() || source.title : source.title, source.type)) { cancel(); setQuery(''); setName(''); setResolved(null); setError(''); editedName.current = false; setKind('default') } } finally { submitting.current = false } }
  return <section className="subscription-add-compact" aria-label="添加订阅来源">
    <form onSubmit={event => { event.preventDefault(); void submit() }}><div className="add-input-row">
      <select aria-label="订阅来源类型" value={kind} disabled={creating} onChange={event => { cancel(); setKind(event.target.value as typeof kind); setQuery(''); setResolved(null); setError('') }}><option value="default">普通订阅</option><option value="arxiv">arXiv 分类</option><option value="huggingface">HF 每日论文</option></select>
      {kind === 'arxiv' && <select aria-label="常用 arXiv 分类" value={commonArxivCategories.includes(query) ? query : 'custom'} disabled={creating} onChange={event => changeQuery(event.target.value === 'custom' ? '' : event.target.value)}><option value="custom">自定义分类</option>{commonArxivCategories.map(value => <option key={value} value={value}>{value}</option>)}</select>}
      <div className="add-input-wrapper"><Link2 size={16} aria-hidden="true" /><input id="subscription-query" ref={inputRef} aria-label="订阅地址" placeholder={kind === 'arxiv' ? '例如 cs.AI 或 cs.AI+cs.CL' : kind === 'huggingface' ? '日期 YYYY-MM-DD，留空为今天' : '粘贴订阅链接、频道地址或 UP 主 UID'} value={query} maxLength={2048} required={kind === 'default' || kind === 'arxiv'} disabled={creating} onChange={event => changeQuery(event.target.value)} /></div>
      {query.trim() && <div className="add-input-wrapper add-name-input"><input aria-label="订阅来源名称" placeholder="自动读取名称" value={name} maxLength={100} disabled={creating} onChange={event => { editedName.current = true; setName(event.target.value) }} /></div>}
      <button className="button-add-submit" type="submit" aria-label={loading ? '正在识别订阅' : '保存来源'} disabled={busy || loading || kind !== 'huggingface' && !query.trim()}>{creating || loading ? <LoaderCircle className="spin" size={18} aria-hidden="true" /> : <Plus size={18} aria-hidden="true" />}<span>{creating ? '添加中' : loading ? '识别中' : '添加'}</span></button>
      {loading && <button type="button" className="icon-button" aria-label="取消识别订阅" title="取消识别" onClick={cancel}><X size={18} aria-hidden="true" /></button>}
    </div></form>
    {error && <p className="add-error" role="alert"><AlertCircle size={14} aria-hidden="true" />{error}</p>}
    {resolved && <div className="add-source-confirm" role="status"><Check size={14} aria-hidden="true" /><span>{sourceLabels[resolved.type]}</span><strong>{resolved.title}</strong></div>}
  </section>
}
