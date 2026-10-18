import { CheckCheck, ChevronDown, ChevronRight, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { filterModels, getModelCapabilities, getModelProviderStats, getVisibleModelRows, setModelsEnabled, sortModelsWithEnabledFirst, type ModelCapability } from './modelPicker'

const labels: Record<ModelCapability, string> = { text: '文本', vision: '视觉', reasoning: '推理', 'translation-only': '仅翻译' }

export function ModelPicker({ availableModels, enabledModels, manualModel, disabled, onEnabledModelsChange, onManualModelChange, onAddManualModel }: {
  availableModels: string[]; enabledModels: string[]; manualModel: string; disabled?: boolean
  onEnabledModelsChange: (models: string[]) => void; onManualModelChange: (value: string) => void; onAddManualModel: () => void
}) {
  const [query, setQuery] = useState('')
  const [collapsed, setCollapsed] = useState<Set<string> | null>(null)
  const [visibleCounts, setVisibleCounts] = useState<Record<string, number>>({})
  const allModels = useMemo(() => sortModelsWithEnabledFirst([...availableModels, ...enabledModels], enabledModels), [availableModels, enabledModels])
  const matches = useMemo(() => filterModels(allModels, query), [allModels, query])
  const enabled = useMemo(() => new Set(enabledModels), [enabledModels])
  const selected = matches.filter(model => enabled.has(model))
  const groups = useMemo(() => getModelProviderStats(matches, enabledModels), [matches, enabledModels])
  const update = (models: string[], value: boolean) => onEnabledModelsChange(setModelsEnabled(models, enabledModels, value))
  return <div className="model-picker">
    <div className="model-picker-toolbar"><label className="model-picker-search"><Search size={15}/><span className="sr-only">搜索模型</span><input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索模型 ID" aria-label="搜索模型 ID" disabled={disabled}/></label><div className="model-picker-actions"><button type="button" className="text-button" onClick={() => update(allModels, true)} disabled={disabled || !allModels.length}><CheckCheck size={14}/>全部启用</button><button type="button" className="text-button" onClick={() => update(allModels, false)} disabled={disabled || !allModels.length}>全部取消</button>{query && <button type="button" className="text-button" onClick={() => update(matches, !matches.every(model => enabled.has(model)))} disabled={disabled || !matches.length}>{matches.every(model => enabled.has(model)) ? '取消搜索结果' : '启用搜索结果'}</button>}</div></div>
    <div className="model-picker-meta"><span>{allModels.length} 个模型</span><span>已启用 {allModels.filter(model => enabled.has(model)).length}</span>{query && <span>匹配 {matches.length}</span>}</div>
    <div className="model-picker-list" role="list" aria-label="可用模型">
      {selected.length > 0 && <section className="model-picker-enabled"><div className="model-picker-section-heading"><h3>已启用 <span>{selected.length}</span></h3><button type="button" className="text-button" onClick={() => update(selected, false)} disabled={disabled}>全部取消</button></div>{selected.map(model => <ModelOption key={model} model={model} checked onChange={value => update([model], value)} disabled={Boolean(disabled)}/>)}</section>}
      {groups.map(group => { const open = Boolean(query) || Boolean(collapsed && !collapsed.has(group.provider)); const allGroupModels = group.models.filter(model => !enabled.has(model)); const groupRows = getVisibleModelRows(allGroupModels, open, visibleCounts[group.provider] ?? 50); const canShowMore = open && groupRows.length < allGroupModels.length; return <section className="model-picker-group" key={group.provider}><div className="model-picker-group-heading"><button type="button" className="model-picker-group-toggle" aria-expanded={open} onClick={() => setCollapsed(current => { const next = new Set(current ?? (query ? [] : groups.map(item => item.provider))); next.has(group.provider) ? next.delete(group.provider) : next.add(group.provider); return next })}><span>{open ? <ChevronDown size={15}/> : <ChevronRight size={15}/>}</span><strong>{group.provider}</strong><span className="model-picker-count">{group.models.length} 个，已启用 {group.enabledCount}</span></button><span className="model-picker-group-actions"><button type="button" className="text-button" onClick={() => update(group.models, true)} disabled={disabled || group.enabledCount === group.models.length}>全部启用</button><button type="button" className="text-button" onClick={() => update(group.models, false)} disabled={disabled || group.enabledCount === 0}>全部取消</button></span></div>{groupRows.map(model => <ModelOption key={model} model={model} checked={false} onChange={value => update([model], value)} disabled={Boolean(disabled)}/>)}{canShowMore && <button type="button" className="text-button model-picker-more" onClick={() => setVisibleCounts(current => ({ ...current, [group.provider]: (current[group.provider] ?? 50) + 50 }))}>显示更多</button>}</section>})}
      {!matches.length && <p className="model-picker-empty">没有找到匹配的模型，请换个关键词。</p>}
    </div>
    <div className="ai-model-manual model-picker-manual"><input aria-label="手动添加模型名" value={manualModel} onChange={event => onManualModelChange(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); onAddManualModel() } }} placeholder="手动添加模型名" maxLength={200} disabled={disabled}/><button type="button" className="button button-secondary" onClick={onAddManualModel} disabled={disabled || !manualModel.trim()}>添加模型</button></div>
  </div>
}

function ModelOption({ model, checked, onChange, disabled }: { model: string; checked: boolean; onChange: (value: boolean) => void; disabled: boolean }) {
  return <label className="model-picker-row" title={model} role="listitem"><input type="checkbox" checked={checked} onChange={event => onChange(event.target.checked)} disabled={disabled}/><span className="model-picker-name">{model}</span><span className="model-picker-badges">{getModelCapabilities(model).map(capability => <span className={`model-capability model-capability-${capability}`} key={capability}>{labels[capability]}</span>)}</span><span className="model-picker-state">{checked ? '已启用' : '未启用'}</span></label>
}
