import { Check, LoaderCircle, PlugZap, RefreshCw, Save, X, ListRestart, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { AICheckResult, AITask, AISettings as Settings } from './types'
import './ai.css'

const DEFAULT_SUMMARY_PROMPT = '请用简体中文总结提供的内容，同时输出一句话总结、关键要点和层级大纲。大纲保留原文段落锚点；有时间戳时请关联时间戳。只引用输入中的索引，不要编造事实。'

export function AISettings() {
  const [saved, setSaved] = useState<Settings | null>(null)
  const [baseUrl, setBaseUrl] = useState(''), [model, setModel] = useState(''), [key, setKey] = useState(''), [noKey, setNoKey] = useState(false)
  const [summaryModel, setSummaryModel] = useState('')
  const [chatModel, setChatModel] = useState('')
  const [enabledModels, setEnabledModels] = useState<string[]>([])
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [manualModel, setManualModel] = useState('')
  const [summaryPrompt, setSummaryPrompt] = useState('')
  const [summaryContextBudget, setSummaryContextBudget] = useState(28000)
  const [protocol, setProtocol] = useState<Settings['protocol']>('openai')
  const [pending, setPending] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const [checking, setChecking] = useState<AITask | null>(null)
  const [confirmReload, setConfirmReload] = useState(false)
  const operation = useRef<AbortController | null>(null)
  async function load() {
    operation.current?.abort(); const current = new AbortController(); operation.current = current
    setPending(true); setError(''); setNotice(''); setConfirmReload(false)
    try {
      const result = await request<Settings>('/ai/settings', { signal: current.signal })
      if (!current.signal.aborted) { setSaved(result); setProtocol(result.protocol); setBaseUrl(result.baseUrl); setModel(result.model); setChatModel(result.chatModel ?? result.model); setEnabledModels(result.enabledModels ?? (result.model ? [result.model] : [])); setAvailableModels(result.availableModels ?? (result.model ? [result.model] : [])); setManualModel(''); setSummaryModel(result.summaryModel ?? ''); setSummaryPrompt(result.summaryPrompt || DEFAULT_SUMMARY_PROMPT); setSummaryContextBudget(result.summaryContextBudget ?? 28000); setKey(''); setNoKey(false) }
    } catch (cause) { if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : 'AI 设置暂不可用。') }
    finally { if (!current.signal.aborted) setPending(false) }
  }
  useEffect(() => { void load(); return () => operation.current?.abort() }, [])
  async function save(event: React.FormEvent) {
    event.preventDefault(); if (!saved || pending) return
    const current = new AbortController(); operation.current = current
    setPending(true); setError(''); setNotice(''); setConfirmReload(false)
    try {
      const result = await request<Settings>('/ai/settings', { method: 'PUT', signal: current.signal, body: JSON.stringify({ protocol, baseUrl, model, chatModel, enabledModels, availableModels, summaryModel, summaryPrompt, summaryContextBudget, revision: saved.revision, ...(noKey ? { key: '' } : key.trim() ? { key } : {}) }) })
      if (!current.signal.aborted) { setSaved(result); setProtocol(result.protocol); setBaseUrl(result.baseUrl); setModel(result.model); setChatModel(result.chatModel ?? result.model); setEnabledModels(result.enabledModels ?? []); setAvailableModels(result.availableModels ?? []); setManualModel(''); setSummaryModel(result.summaryModel ?? ''); setSummaryPrompt(result.summaryPrompt || DEFAULT_SUMMARY_PROMPT); setSummaryContextBudget(result.summaryContextBudget ?? 28000); setKey(''); setNoKey(false); setNotice(result.ready || result.summaryReady ? '设置已保存。' : '设置已保存；未选择模型，翻译尚未启用。') }
    } catch (cause) { if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : 'AI 设置未保存。') }
    finally { if (!current.signal.aborted) setPending(false) }
  }
  const changedEndpoint = saved?.hasKey && baseUrl.trim().replace(/\/+$/, '') !== saved.baseUrl
  const requiresKey = changedEndpoint && !key.trim() && !noKey
  const dirty = saved && (baseUrl.trim().replace(/\/+$/, '') !== saved.baseUrl || model.trim() !== saved.model || chatModel.trim() !== (saved.chatModel ?? saved.model) || JSON.stringify(enabledModels) !== JSON.stringify(saved.enabledModels ?? [saved.model]) || summaryModel.trim() !== (saved.summaryModel ?? '') || summaryPrompt.trim() !== (saved.summaryPrompt || DEFAULT_SUMMARY_PROMPT) || summaryContextBudget !== (saved.summaryContextBudget ?? 28000) || protocol !== saved.protocol || key !== '' || noKey && saved.hasKey)
  async function loadModels() { if (!saved || pending) return; setPending(true); setError(''); try { const result = await request<{ models: string[] }>('/ai/models', { method: 'POST', body: JSON.stringify({ protocol, baseUrl, ...(key.trim() ? { key } : {}) }) }); setAvailableModels(result.models); setEnabledModels(current => current.length ? current.filter(model => result.models.includes(model)) : result.models); setNotice(`已获取 ${result.models.length} 个模型，请勾选后保存。`) } catch (cause) { setError(cause instanceof Error ? cause.message : '获取模型失败。') } finally { setPending(false) } }
  function addManualModel() { const value = manualModel.trim(); if (!value || availableModels.includes(value)) return; setAvailableModels(current => [...current, value]); setEnabledModels(current => current.includes(value) ? current : [...current, value]); setModel(current => current || value); setChatModel(current => current || value); setManualModel('') }
  async function check(task: AITask) {
    if (!saved || pending || dirty) return
    const current = new AbortController(); operation.current = current
    setPending(true); setChecking(task); setError(''); setNotice('')
    try {
      const result = await request<AICheckResult>('/ai/check', { method: 'POST', signal: current.signal, body: JSON.stringify({ task, revision: saved.revision }) })
      if (!current.signal.aborted) setNotice(`${task === 'translation' ? '翻译' : '摘要'}连接正常：${result.model}，${(result.elapsedMs / 1000).toFixed(1)} 秒。`)
    } catch (cause) {
      if (!current.signal.aborted) setError(`${task === 'translation' ? '翻译' : '摘要'}检测失败：${cause instanceof Error ? cause.message : '请检查接口和模型。'}`)
    } finally {
      if (!current.signal.aborted) { setPending(false); setChecking(null) }
    }
  }
  return <section className="settings-section ai-settings" aria-labelledby="ai-settings-title">
    <div className="settings-heading"><div><h2 id="ai-settings-title">翻译摘要</h2><p className="field-hint">连接你的模型服务，用于内容翻译与阅读摘要。</p></div><button className="icon-button" aria-label="刷新" title="刷新 AI 设置" disabled={pending} onClick={() => { if (dirty) setConfirmReload(true); else void load() }}><RefreshCw size={16}/></button></div>
    {confirmReload && <div className="cookie-confirm" role="group" aria-label="放弃未保存的设置"><p>刷新会丢弃未保存的修改。</p><div><button type="button" className="button button-secondary" onClick={()=>setConfirmReload(false)}>继续编辑</button><button type="button" className="button button-quiet" onClick={()=>void load()}>放弃修改</button></div></div>}
    {saved && <form className="ai-config" onSubmit={event => void save(event)}>
      <fieldset className="settings-form-section"><legend>服务连接</legend><div className="settings-form-fields">
      <label>接口地址<input type="url" value={baseUrl} onChange={event => setBaseUrl(event.target.value)} placeholder="https://api.openai.com/v1" autoComplete="off" maxLength={2048} required disabled={pending} /></label>
      <fieldset className="layout-preference ai-config-wide"><legend>连接方式</legend><div className="layout-options">
        {([{value:'openai',label:'标准接口'},{value:'compatible',label:'兼容接口'}] as const).map(option=><label key={option.value}><input type="radio" name="ai-protocol" value={option.value} checked={protocol===option.value} disabled={pending} onChange={()=>setProtocol(option.value)}/><span>{option.label}</span></label>)}
      </div></fieldset>
      <label>API 密钥<input type="password" value={key} onChange={event => setKey(event.target.value)} placeholder={saved.hasKey ? '已保存；同一接口留空保留' : '本地服务可留空'} maxLength={4096} autoComplete="new-password" disabled={pending || noKey} /></label>
      <label className="ai-no-key"><input type="checkbox" checked={noKey} onChange={event => { setNoKey(event.target.checked); setKey('') }} disabled={pending} />无需密钥（清除旧密钥）</label>
      {requiresKey && <p className="field-hint ai-config-wide">更换接口后，请填写该服务的密钥，或勾选不需要密钥。</p>}
      </div></fieldset>
      <fieldset className="settings-form-section"><legend>使用模型</legend><div className="settings-form-fields settings-model-fields">
      <div className="ai-model-fetch"><button type="button" className="button button-secondary" onClick={() => void loadModels()} disabled={pending}><ListRestart size={14}/>获取模型</button><span className="field-hint">也可手动添加模型名</span></div>
      <fieldset className="ai-model-list ai-config-wide"><legend>启用模型</legend>{availableModels.map(value => <label key={value}><input type="checkbox" checked={enabledModels.includes(value)} onChange={event => setEnabledModels(current => event.target.checked ? [...current, value] : current.filter(model => model !== value))} disabled={pending}/>{value}</label>)}<div className="ai-model-manual"><input aria-label="手动添加模型名" value={manualModel} onChange={event => setManualModel(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); addManualModel() } }} placeholder="手动添加模型名" maxLength={200} autoComplete="off" disabled={pending}/><button type="button" className="icon-button" title="添加模型" aria-label="添加模型" onClick={addManualModel} disabled={pending || !manualModel.trim()}><Plus size={14}/></button></div></fieldset>
      <label>翻译模型<select value={model} onChange={event => setModel(event.target.value)} disabled={pending}><option value="">选择已启用模型</option>{enabledModels.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
      <label>对话模型<select value={chatModel} onChange={event => setChatModel(event.target.value)} disabled={pending}><option value="">选择已启用模型</option>{enabledModels.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
      <label>摘要模型<select value={summaryModel} onChange={event => setSummaryModel(event.target.value)} disabled={pending}><option value="">使用翻译模型</option>{enabledModels.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
      <label className="ai-config-wide ai-prompt-field">摘要提示词<textarea aria-label="摘要提示词" value={summaryPrompt} onChange={event => setSummaryPrompt(event.target.value)} placeholder="留空使用默认提示词" maxLength={12000} rows={5} disabled={pending}/><span className="field-hint">可按你的阅读习惯调整摘要重点。</span><button type="button" className="text-button" onClick={() => setSummaryPrompt(DEFAULT_SUMMARY_PROMPT)} disabled={pending || summaryPrompt === DEFAULT_SUMMARY_PROMPT}>恢复默认</button></label>
      <label>摘要上下文预算<input type="number" min={2000} max={64000} step={1000} value={summaryContextBudget} onChange={event => setSummaryContextBudget(Number(event.target.value) || 28000)} disabled={pending}/><span className="field-hint">默认 28000 字节，内容超出后才会分段。</span></label>
      {saved.ready && saved.summaryReady === false && <p className="field-hint ai-config-wide">当前模型可用于翻译；生成摘要需填写通用摘要模型。</p>}
      </div></fieldset>
      <div className="settings-savebar"><span className="settings-state" data-dirty={Boolean(dirty)}>{dirty ? '未保存更改' : saved.ready ? <><Check size={14}/>已选模型</> : '未选模型'}</span><button aria-label="保存 AI 设置" className="button button-primary" type="submit" disabled={pending || !dirty || !!requiresKey}>{pending && !checking ? <LoaderCircle size={15} className="spin"/> : <Save size={15}/>} {pending && !checking ? '保存中' : '保存设置'}</button></div>
    </form>}
    {saved && <div className="ai-check-actions">
      {(['translation', 'summary'] as const).map(task => <button key={task} aria-label={`检测${task === 'translation' ? '翻译' : '摘要'}连接`} type="button" className="button button-secondary" title={dirty ? '先保存修改后的设置' : undefined} disabled={pending || Boolean(dirty) || saved.translating || !(task === 'translation' ? saved.ready : saved.summaryReady)} onClick={() => void check(task)}>{checking === task ? <LoaderCircle size={14} className="spin"/> : <PlugZap size={14}/>}测试{task === 'translation' ? '翻译' : '摘要'}</button>)}
      {checking && <button type="button" className="text-button" onClick={() => { operation.current?.abort(); setPending(false); setChecking(null); setNotice('检测已取消。') }}><X size={14}/>取消检测</button>}
    </div>}
    {checking && <p className="field-hint" role="status">正在检测{checking === 'translation' ? '翻译' : '摘要'}连接…</p>}
    {pending && !saved && <p className="field-hint" role="status">正在读取 AI 设置…</p>}
    {notice && !dirty && <p className="settings-feedback" role="status"><Check size={14} />{notice}</p>}
    {error && <p className="settings-feedback" role="alert">{error}</p>}
    <details className="settings-details"><summary>数据发送与费用</summary><p>启用模型后，可见卡片的外文标题与摘要会自动发送到所选服务翻译；摘要最多发送前 280 个字符。阅读页的标题、文章、字幕翻译与 AI 摘要由你点击后发送对应原文。连接检测只发送一段固定测试文字，不读取文章或笔记。已有缓存会复用，不发送视频、平台 Cookie 或笔记。第三方服务可能收费，取消前已处理的请求可能仍产生费用。密钥保存在运行 Tangentry 的机器上，页面只显示保存状态。</p></details>
    <details className="settings-details"><summary>连接说明</summary><p>字幕分批翻译，失败后不自动重试。</p><p>支持 Chat Completions 接口，可填写自己的 HTTPS 接口或本机 HTTP 地址。通用模型使用 JSON 输出；Index-Translate 与 doubao-translate 使用编号分隔的批量纯文本翻译。摘要模型需支持指令与 JSON 输出，与翻译共用接口和密钥。兼容服务或本机 Ollama 可选择「兼容服务 / 本地模型」。保存配置不会调用模型。</p></details>
  </section>
}
