import { Check, Download, LoaderCircle, RotateCcw, SkipForward } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useCapabilities } from '../../shared/api/desktop'
import '../notes/markdown.css'
import { SettingsTask } from './ui/SettingsActionPage'

export type UpdaterStatus = {
  enabled: boolean
  currentVersion: string
  available: boolean
  version?: string
  notes?: string
  date?: string
  state: 'idle' | 'checking' | 'available' | 'downloading' | 'installed' | 'error'
  progress: number
  error?: string
  automaticChecks: boolean
  lastCheck?: string
  skippedVersion?: string
  relaunchRequired: boolean
}

export type MyGoRuntime = {
  call<T>(method: string, ...args: unknown[]): Promise<T>
  on<T>(event: string, listener: (payload: T) => void): () => void
}

export function runtime(): MyGoRuntime | null {
  return (window as Window & { mygo?: MyGoRuntime }).mygo ?? null
}

const initial: UpdaterStatus = {
  enabled: false, currentVersion: '开发版', available: false, state: 'idle', progress: 0,
  automaticChecks: true, relaunchRequired: false,
}

function snoozeUpdaterNotice() {
  window.dispatchEvent(new Event('tangentry-updater-snooze'))
}

export function UpdaterSettings() {
  const capabilities = useCapabilities()
  const [status, setStatus] = useState<UpdaterStatus>(initial)
  const [message, setMessage] = useState('')
  const [pending, setPending] = useState(false)
  const isDesktop = capabilities.updater && Boolean(runtime())

  const refresh = useCallback(async () => {
    const rt = runtime()
    if (!rt) return
    try { setStatus(await rt.call<UpdaterStatus>('Updater.Status')) } catch (error) { setMessage(error instanceof Error ? error.message : '无法读取更新设置。') }
  }, [])

  useEffect(() => {
    if (!isDesktop) return
    void refresh()
    const off = runtime()?.on<UpdaterStatus>('tangentry-updater', setStatus)
    return off
  }, [isDesktop, refresh])

  const run = async (method: string, ...args: unknown[]) => {
    const rt = runtime()
    if (!rt) return
    setPending(true); setMessage('')
    try { setStatus(await rt.call<UpdaterStatus>(method, ...args)) }
    catch (error) { setMessage(error instanceof Error ? error.message : '更新操作失败，请稍后重试。'); await refresh() }
    finally { setPending(false) }
  }

  const date = useMemo(() => status.date ? new Date(status.date).toLocaleString('zh-CN') : '', [status.date])
  if (capabilities.server) return <SettingsTask title="服务端版本" description="服务端版本通过 Docker 镜像更新，请拉取新镜像后重新创建容器。" actions={<span className="settings-inline-status">服务端模式</span>}><p className="field-hint">更新由部署环境负责。</p></SettingsTask>
  if (!isDesktop) return <SettingsTask title="Web 版本" description="当前运行在 Web 模式，版本更新由部署环境负责。" actions={<span className="settings-inline-status">Web 模式</span>}><p className="field-hint">请在部署 Tangentry 的环境中更新版本。</p></SettingsTask>

  return <SettingsTask title="应用更新" description={`当前版本 ${status.currentVersion || '开发版'}`} actions={<button type="button" className="button button-secondary" disabled={pending || status.state === 'checking'} onClick={() => void run('Updater.Check', true)}>{status.state === 'checking' ? <LoaderCircle size={15} className="spin"/> : <Check size={15}/>}检查更新</button>}>
    <div className="setting-row"><div><strong>自动检查更新</strong><p>启动后检查一次，之后每 6 小时检查。</p></div><label className="card-switch"><input type="checkbox" checked={status.automaticChecks} disabled={pending} onChange={event => void run('Updater.SetAutomaticChecks', event.target.checked)}/><span>启用</span></label></div>
    {!status.enabled && <p className="field-hint" role="status">开发构建或不可写的安装位置不会自动更新。</p>}
    {message && <p className="error-panel" role="alert">{message}</p>}
    {status.available && <div className="updater-release" role="status"><div className="updater-release-head"><div><strong>发现新版本 {status.version}</strong>{date && <small>{date}</small>}</div><span>当前 {status.currentVersion}</span></div>
      {status.notes && <div className="updater-notes markdown-body"><ReactMarkdown remarkPlugins={[remarkGfm]}>{status.notes}</ReactMarkdown></div>}
      {status.state === 'downloading' && <div className="updater-progress"><progress max="1" value={status.progress}/><span>{Math.round(status.progress * 100)}%</span></div>}
      {status.state === 'installed' || status.relaunchRequired ? <button type="button" className="button button-primary" disabled={pending} onClick={() => void run('Updater.Relaunch')}><RotateCcw size={15}/>重启以完成更新</button> : <div className="updater-actions"><button type="button" className="button button-primary" disabled={pending} onClick={() => void run('Updater.Install')}><Download size={15}/>{status.state === 'downloading' ? '正在下载…' : '下载并安装'}</button><button type="button" className="button button-secondary" disabled={pending} onClick={snoozeUpdaterNotice}>稍后</button><button type="button" className="text-button" disabled={pending} onClick={() => void run('Updater.Skip', status.version)}><SkipForward size={14}/>跳过此版本</button></div>}
    </div>}
    {status.lastCheck && <p className="field-hint updater-last-check">上次检查：{new Date(status.lastCheck).toLocaleString('zh-CN')}</p>}
  </SettingsTask>
}
