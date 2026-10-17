import { Folder, Pencil, Plus, RefreshCw, Search, Trash2, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { SubscriptionGroup, SubscriptionSource } from '../../shared/api/types'
import { sourceLabel } from './sourcePresentation'
import type { useSubscriptions } from './useSubscriptions'
import type { useSubscriptionGroups } from './useSubscriptionGroups'
import { SubscriptionSourceIcon } from './SubscriptionSourceIcon'

const collapseKey = 'tangentry-subscription-collapsed-groups'
type Groups = ReturnType<typeof useSubscriptionGroups>

function GroupEditor({group, state, sourceBusy}: {group: SubscriptionGroup; state: Groups; sourceBusy: boolean}) {
  const [mode, setMode] = useState<'rename' | 'delete' | null>(null)
  const [draft, setDraft] = useState(group.name)
  const [baseline, setBaseline] = useState(group)
  const input = useRef<HTMLInputElement>(null)
  const busy = Boolean(state.pending) || state.loading || Boolean(state.listError) || sourceBusy
  useEffect(() => { if (mode === 'rename') input.current?.focus() }, [mode])
  const changed = baseline.revision !== group.revision
  const error = state.error?.id === group.id ? state.error.message : ''
  return <div className="subscription-group-editor">
    <div className="subscription-group-tools"><button className="icon-button" aria-label={`重命名分组 ${group.name}`} disabled={busy} onClick={() => { setBaseline(group); setDraft(group.name); setMode('rename') }}><Pencil size={14} aria-hidden="true" /></button><button className="icon-button" aria-label={`解散分组 ${group.name}`} disabled={busy} onClick={() => { setBaseline(group); setMode('delete') }}><Trash2 size={14} aria-hidden="true" /></button></div>
    {mode === 'rename' && <form onSubmit={event => { event.preventDefault(); void state.rename(baseline, draft).then(ok => { if (ok) setMode(null) }) }}><label className="subscription-field"><span>分组新名称</span><div className="subscription-input"><input ref={input} aria-label={`分组新名称 ${group.name}`} value={draft} onChange={event => setDraft(event.target.value)} maxLength={120} required /></div></label><div className="subscription-group-form-actions"><button className="button button-secondary" disabled={busy || !draft.trim() || [...draft.trim()].length > 60 || changed}>保存分组名称</button><button type="button" className="icon-button" aria-label="取消修改分组" disabled={busy} onClick={() => setMode(null)}><X size={15} aria-hidden="true" /></button></div></form>}
    {mode === 'delete' && <div className="subscription-group-confirm"><p>解散「{group.name}」？来源将回到未分组，内容和阅读记录保留。</p><div className="subscription-group-form-actions"><button className="button button-secondary" disabled={busy || changed} onClick={() => void state.remove(baseline)}>确认解散</button><button className="text-button" disabled={busy} onClick={() => setMode(null)}>保留分组</button></div></div>}
    {mode && changed && <p className="subscription-group-feedback" role="status">分组已在其他页面更新。<button className="text-button" onClick={() => setBaseline(group)}>使用当前版本</button></p>}
    {error && <p className="subscription-group-feedback" role="alert">{error}</p>}
  </div>
}

export function SubscriptionSources({state, groups, onSelect}: {state: ReturnType<typeof useSubscriptions>; groups: Groups; onSelect?: () => void}) {
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [filter, setFilter] = useState('')
  const newName = useRef<HTMLInputElement>(null)
  const createTrigger = useRef<HTMLButtonElement>(null)
  const closeCreate = () => { setAdding(false); requestAnimationFrame(() => createTrigger.current?.focus({preventScroll:true})) }
  const [collapsed, setCollapsed] = useState<Set<string>>(() => { try { const value: unknown = JSON.parse(sessionStorage.getItem(collapseKey) ?? '[]'); return new Set(Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : []) } catch { return new Set() } })
  useEffect(() => { try { sessionStorage.setItem(collapseKey, JSON.stringify([...collapsed])) } catch { /* Navigation works without session storage. */ } }, [collapsed])
  const selectedGroup = state.sources.find(source => source.id === state.selectedId)?.groupId
  useEffect(() => { if (selectedGroup) setCollapsed(current => { if (!current.has(selectedGroup)) return current; const next = new Set(current); next.delete(selectedGroup); return next }) }, [selectedGroup, state.selectedId])
  useEffect(() => { if (adding) newName.current?.focus() }, [adding])
  const knownGroups = useMemo(() => new Set(groups.groups.map(group => group.id)), [groups.groups])
  const keyword = filter.trim().toLocaleLowerCase()
  const matches = (source: SubscriptionSource, groupName = '') => !keyword || `${source.name} ${sourceLabel(source)} ${groupName}`.toLocaleLowerCase().includes(keyword)
  const sourceButton = (source: SubscriptionSource) => <button key={source.id} className={`subscription-source ${source.id === state.selectedId ? 'active' : ''}`} aria-current={source.id === state.selectedId ? 'true' : undefined} title={source.name} onClick={() => { state.select(source.id); onSelect?.() }}><SubscriptionSourceIcon source={source}/><strong>{source.name}</strong><span className="subscription-source-platform">{sourceLabel(source)}</span><small>{source.lastUpdatedAt ? `${source.itemCount} 条` : '未更新'}</small></button>
  const ungrouped = state.sources.filter(source => !source.groupId)
  const unresolved = state.sources.filter(source => source.groupId && !knownGroups.has(source.groupId))
  const groupBusy = groups.loading || Boolean(groups.listError) || Boolean(groups.pending)
  const reload = () => { void state.reload(); void groups.reload() }
  return <aside className="subscription-sources" aria-label="已保存来源">
    <div className="subscription-section-heading"><h2>我的来源 <span className="count">{state.sources.length}</span></h2><div className="subscription-source-tools"><button ref={createTrigger} className="icon-button" disabled={groupBusy} aria-label="新建来源分组" onClick={() => { if (adding) closeCreate(); else setAdding(true) }}><Plus size={16} aria-hidden="true" /></button><button className="icon-button" disabled={state.loading || Boolean(state.pending) || Boolean(groups.pending)} aria-label="重新加载来源" onClick={reload}><RefreshCw size={16} aria-hidden="true" /></button></div></div>
    {adding && <form className="subscription-group-create" onSubmit={event => { event.preventDefault(); void groups.create(name).then(ok => { if (ok) { setName(''); closeCreate() } }) }}><label className="subscription-field"><span>分组名称（最多 60 字）</span><div className="subscription-input"><input ref={newName} aria-label="新分组名称" value={name} maxLength={120} onChange={event => setName(event.target.value)} required placeholder="例如：科技、日常、播客" /></div></label><div className="subscription-group-form-actions"><button className="button button-secondary" disabled={groupBusy || !name.trim() || [...name.trim()].length > 60}>创建分组</button><button className="icon-button" type="button" aria-label="取消创建分组" disabled={Boolean(groups.pending)} onClick={closeCreate}><X size={15} aria-hidden="true" /></button></div>{groups.error?.kind === 'create' && <p className="subscription-group-feedback" role="alert">{groups.error.message}</p>}</form>}
    <label className="subscription-field subscription-source-search"><span className="sr-only">筛选订阅来源</span><div className="subscription-input"><Search size={14} aria-hidden="true"/><input type="search" aria-label="筛选订阅来源" placeholder="找来源或分组" value={filter} onChange={event => setFilter(event.target.value)} /></div></label>
    {state.loading && <p role="status" className="field-hint">正在加载来源…</p>}
    {groups.loading && <p className="field-hint" role="status">正在读取分组…</p>}
    {groups.listError && <p className="subscription-group-feedback" role="alert">{groups.listError}<button className="text-button" onClick={reload}>重新加载分组</button></p>}
    {groups.notice && <p className="subscription-group-feedback" role="status">{groups.notice}</p>}
    <nav aria-label="选择订阅来源">
      {groups.groups.map(group => {
        const members = state.sources.filter(source => source.groupId === group.id)
        const visible = members.filter(source => matches(source, group.name))
        return <details className="subscription-source-group" key={group.id} open={keyword !== '' || !collapsed.has(group.id)} onToggle={event => { if (keyword) return; const open = event.currentTarget.open; setCollapsed(current => { if (current.has(group.id) === !open) return current; const next = new Set(current); if (open) next.delete(group.id); else next.add(group.id); return next }) }}>
          <summary aria-label={`分组 ${group.name}，${members.length} 个来源`}><Folder size={15} aria-hidden="true" /><span>{group.name}</span><span className="count">{members.length}</span></summary>
          <div className="subscription-group-members">{visible.map(sourceButton)}{visible.length === 0 && <p className="field-hint">{keyword ? '没有匹配的来源' : '在来源详情中选择这个分组。'}</p>}</div>
          <details className="subscription-group-manage"><summary>管理分组 {group.name}</summary><GroupEditor group={group} state={groups} sourceBusy={state.loading || Boolean(state.listError) || Boolean(state.pending)} /></details>
        </details>
      })}
      <div className="subscription-ungrouped"><h3>未分组 <span className="count">{ungrouped.length}</span></h3>{ungrouped.filter(source => matches(source)).map(sourceButton)}{ungrouped.length > 0 && !ungrouped.some(source => matches(source)) && <p className="field-hint">没有匹配的来源</p>}</div>
      {unresolved.length > 0 && <div className="subscription-ungrouped"><h3>分组待读取</h3>{unresolved.filter(source => matches(source)).map(sourceButton)}</div>}
    </nav>
    {!state.loading && !state.listError && state.sources.length === 0 && <p className="field-hint">保存一个来源，之后可以随时回来。</p>}
  </aside>
}
