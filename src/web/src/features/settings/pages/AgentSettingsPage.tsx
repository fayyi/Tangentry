import { Activity, BookOpen, Bot, CircleCheck, CircleOff, ClipboardPenLine, Copy, Globe2, Image, NotebookPen, Plug, RotateCw, ShieldCheck, Sparkles, StickyNote, type LucideIcon } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { request } from '../../../shared/api/client'
import { SettingsActionPage, SettingsTask } from '../ui/SettingsActionPage'
import '../agent-settings.css'

type Scope = { id: string; label: string; hint: string; icon: LucideIcon }
const scopes: Scope[] = [
  { id: 'content_read', label: '内容读取', hint: '搜索、详情、字幕、收件箱和社交素材', icon: BookOpen },
  { id: 'network_fetch', label: '联网抓取', hint: '按请求访问平台接口，结果会记录在审计中', icon: Globe2 },
  { id: 'image_read', label: '图片与抽帧', hint: '读取已下载媒体中的视频帧', icon: Image },
  { id: 'private_notes_read', label: '私人笔记', hint: '读取本地笔记和字幕引用', icon: StickyNote },
  { id: 'derived_processing', label: '派生处理', hint: '摘要、翻译和转录等后续能力', icon: Sparkles },
  { id: 'agent_write', label: 'Agent 记录写入', hint: '判重、日报、归档和 Agent 草稿', icon: ClipboardPenLine },
]

type ActivityData = { runs: { id: string; client: string; trigger: string; state: string; startedAt: number; error?: string }[]; audit: { runId?: string; action: string; networkFetch: boolean; externalProcessing: boolean; outcome: string; createdAt: number }[] }
type Payload = { settings: { enabled: boolean; scopes: string[] }; status: { running: boolean; protocolVersion: string; endpoint?: string; connectionFile?: string; scopes: string[] }; activity: ActivityData }

function runStateLabel(state: string) { return state === 'completed' ? '已完成' : state === 'failed' ? '失败' : state === 'cancelled' ? '已取消' : '进行中' }
function outcomeLabel(outcome: string) { return outcome === 'ok' ? '成功' : '失败' }
function formatActivityTime(value: number) { return new Date(value * 1000).toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }

export function AgentSettingsPage() {
  const [payload, setPayload] = useState<Payload | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [message, setMessage] = useState('')
  const [copyMessage, setCopyMessage] = useState('')
  const [pending, setPending] = useState(false)

  const load = useCallback(async () => {
    try {
      const next = await request<Payload>('/settings/agent')
      setPayload(next); setEnabled(next.settings.enabled); setSelected(next.settings.scopes)
    } catch (error) { setMessage(error instanceof Error ? error.message : '无法读取 Agent 设置。') }
  }, [])

  const refreshActivity = useCallback(async () => {
    try {
      const next = await request<Payload>('/settings/agent')
      setPayload(current => current ? { ...next, settings: current.settings } : next)
    } catch {
      // Background refresh should not replace a useful page with a transient error.
    }
  }, [])

  useEffect(() => {
    void load()
    const refreshWhenVisible = () => {
      if (document.visibilityState === 'visible') void refreshActivity()
    }
    const timer = window.setInterval(refreshWhenVisible, 10_000)
    document.addEventListener('visibilitychange', refreshWhenVisible)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', refreshWhenVisible)
    }
  }, [load, refreshActivity])

  const save = async () => {
    setPending(true); setMessage('')
    try {
      const next = await request<Payload>('/settings/agent', { method: 'PUT', body: JSON.stringify({ enabled, scopes: selected }) })
      setPayload(next); setMessage(enabled ? 'Agent Gateway 已启用。' : 'Agent Gateway 已停用。')
    } catch (error) { setMessage(error instanceof Error ? error.message : '保存 Agent 设置失败。'); await load() } finally { setPending(false) }
  }
  const copyPath = async () => { if (!payload?.status.connectionFile) return; await navigator.clipboard?.writeText(payload.status.connectionFile); setMessage('连接文件路径已复制。') }
  const copyConfig = async () => {
    if (!payload?.status.connectionFile) return
    const config = { mcpServers: { tangentry: { command: 'tangentry-mcp', args: ['--connection-file', payload.status.connectionFile] } } }
    await navigator.clipboard?.writeText(JSON.stringify(config, null, 2)); setCopyMessage('MCP 配置已复制。')
  }
  const rotateToken = async () => {
    if (!payload) return
    setPending(true); setMessage('')
    try {
      const next = await request<Payload>('/settings/agent', { method: 'PUT', body: JSON.stringify({ enabled: payload.settings.enabled, scopes: payload.settings.scopes }) })
      setPayload(next); setMessage('令牌已轮换，旧连接已失效。')
    } catch (error) { setMessage(error instanceof Error ? error.message : '令牌轮换失败。'); await load() } finally { setPending(false) }
  }
  const running = Boolean(payload?.status.running)

  return <SettingsActionPage className="settings-action-agent" eyebrow="系统" title="Agent 接入" description="让外部 Agent 在本机通过 MCP 使用 Tangentry 的内容研究能力。" status={<span className={`agent-page-status${running ? ' is-live' : ''}`}><span aria-hidden="true" />{running ? '本机服务运行中' : '服务未启动'}</span>} actions={payload?.status.protocolVersion ? <span className="agent-protocol">MCP v{payload.status.protocolVersion}</span> : undefined}>
    <SettingsTask className="agent-task-overview" title={<span className="agent-task-title"><Bot size={16} aria-hidden="true" />本机 MCP Gateway</span>} description="只监听 127.0.0.1。外部 Agent 通过连接文件获取临时令牌，不会接触普通页面接口。">
      <div className="agent-overview"><div className="agent-overview-copy"><span className={`agent-state-line${running ? ' is-live' : ''}`}><span aria-hidden="true" />{running ? 'Gateway 已准备好接受 MCP 连接' : '启用后才会生成本机连接文件'}</span><p>建议先开启内容读取和 Agent 记录写入；联网、图片、私人笔记和派生处理可以按需授权。</p></div><label className="agent-toggle"><span><strong>启用 Agent</strong><small>{enabled ? '保存后立即启动 Gateway' : '当前不会接受外部调用'}</small></span><input aria-label="启用 Agent Gateway" type="checkbox" checked={enabled} disabled={pending} onChange={event => setEnabled(event.target.checked)} /></label></div>
      <div className="agent-save-row"><button type="button" className="button button-primary" disabled={pending} onClick={() => void save()}><ShieldCheck size={15} aria-hidden="true" />{pending ? '正在保存…' : '保存接入设置'}</button>{message && <p role="status" className="agent-feedback">{message}</p>}</div>
    </SettingsTask>

    <SettingsTask className="agent-task-scopes" title="授权范围" description="每一项都是独立权限，外部 Agent 不能通过提示词自行扩大范围。" actions={<span className="agent-scope-count">{selected.length} / {scopes.length} 项开启</span>}>
      <div className="agent-scope-grid">{scopes.map(scope => { const Icon = scope.icon; const checked = selected.includes(scope.id); return <label className={`agent-scope-row${checked ? ' is-selected' : ''}`} key={scope.id}><span className="agent-scope-icon"><Icon size={16} aria-hidden="true" /></span><span className="agent-scope-copy"><strong>{scope.label}</strong><small>{scope.hint}</small></span><span className="agent-scope-switch"><input aria-label={scope.label} type="checkbox" checked={checked} disabled={pending || !enabled} onChange={event => setSelected(current => event.target.checked ? [...current, scope.id] : current.filter(value => value !== scope.id))} /><span aria-hidden="true" /></span></label> })}</div>
    </SettingsTask>

    <SettingsTask className="agent-task-connection" title={<span className="agent-task-title"><Plug size={16} aria-hidden="true" />连接 MCP 客户端</span>} description="把这份配置交给 Claude、Codex、Cursor 或其他支持 MCP 的客户端。">
      {payload?.status.connectionFile ? <div className="agent-connection-panel"><div className="agent-connection-heading"><span className="agent-connection-icon"><Plug size={15} aria-hidden="true" /></span><div><strong>连接文件</strong><small>令牌保存在本机，不会显示在页面上</small></div></div><div className="agent-path-row"><code>{payload.status.connectionFile}</code><button type="button" className="icon-button" aria-label="复制连接文件路径" title="复制连接文件路径" onClick={() => void copyPath()}><Copy size={15} aria-hidden="true" /></button></div><div className="agent-connection-actions"><button type="button" className="button button-secondary" onClick={() => void copyConfig()}><Copy size={15} aria-hidden="true" />复制 MCP 配置</button><button type="button" className="button button-secondary" disabled={pending} onClick={() => void rotateToken()}><RotateCw size={15} aria-hidden="true" />轮换令牌</button></div><p className="agent-connection-note">轮换后，已经启动的 MCP 客户端需要重新连接。{copyMessage && <span role="status">{copyMessage}</span>}</p></div> : <div className="agent-connection-empty"><CircleOff size={18} aria-hidden="true" /><div><strong>还没有连接文件</strong><p>启用 Agent 并保存设置后，这里会显示 MCP 客户端配置。</p></div></div>}
    </SettingsTask>

    {payload?.activity && <SettingsTask className="agent-task-activity" title={<span className="agent-task-title"><Activity size={16} aria-hidden="true" />最近活动</span>} description="只显示脱敏后的运行和工具调用记录。"><div className="agent-activity-grid"><div className="agent-activity-column"><div className="agent-activity-heading"><span>运行记录</span><small>{payload.activity.runs.length}</small></div>{payload.activity.runs.length ? <ul>{payload.activity.runs.map(run => <li key={run.id}><span className="agent-activity-marker"><CircleCheck size={14} aria-hidden="true" /></span><div><strong>{run.client || '外部 Agent'}</strong><small>{run.trigger || '本地'} · {formatActivityTime(run.startedAt)}</small>{run.error && <p>{run.error}</p>}</div><em className={run.state === 'completed' ? 'is-success' : run.state === 'failed' ? 'is-danger' : ''}>{runStateLabel(run.state)}</em></li>)}</ul> : <p className="agent-empty-copy">暂无运行记录。</p>}</div><div className="agent-activity-column"><div className="agent-activity-heading"><span>工具审计</span><small>{payload.activity.audit.length}</small></div>{payload.activity.audit.length ? <ul>{payload.activity.audit.map((event, index) => <li key={`${event.createdAt}-${index}`}><span className="agent-activity-marker"><NotebookPen size={14} aria-hidden="true" /></span><div><strong>{event.action}</strong><small>{[event.networkFetch && '联网', event.externalProcessing && '外部处理'].filter(Boolean).join(' · ') || '本地'} · {formatActivityTime(event.createdAt)}</small></div><em className={event.outcome === 'ok' ? 'is-success' : 'is-danger'}>{outcomeLabel(event.outcome)}</em></li>)}</ul> : <p className="agent-empty-copy">暂无工具调用记录。</p>}</div></div></SettingsTask>}
  </SettingsActionPage>
}
