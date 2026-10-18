import { Check, Download, RotateCcw, SkipForward, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useCapabilities } from '../../shared/api/desktop'
import { runtime, type UpdaterStatus } from './UpdaterSettings'
import './settings.css'

export function UpdaterNotice() {
  const capabilities = useCapabilities()
  const [status, setStatus] = useState<UpdaterStatus | null>(null)
  const [hidden, setHidden] = useState(false)
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState('')
  const desktop = capabilities.updater && !capabilities.server && Boolean(runtime())

  useEffect(() => {
    if (!desktop) return
    const rt = runtime()
    if (!rt) return
    void rt.call<UpdaterStatus>('Updater.Status').then(setStatus).catch(() => {})
    const off = rt.on<UpdaterStatus>('tangentry-updater', value => {
      setStatus(value)
      if (value.available) setHidden(false)
    })
    const snooze = () => setHidden(true)
    window.addEventListener('tangentry-updater-snooze', snooze)
    return () => {
      off()
      window.removeEventListener('tangentry-updater-snooze', snooze)
    }
  }, [desktop])

  if (!desktop || !status?.available || hidden) return null

  const act = async (method: string, ...args: unknown[]) => {
    const rt = runtime()
    if (!rt) return
    setPending(true)
    setMessage('')
    try {
      setStatus(await rt.call<UpdaterStatus>(method, ...args))
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '更新操作失败，请稍后重试。')
      try { setStatus(await rt.call<UpdaterStatus>('Updater.Status')) } catch { /* keep the operation error visible */ }
    } finally { setPending(false) }
  }
  const downloading = status.state === 'downloading'
  const installed = status.relaunchRequired || status.state === 'installed'
  const failed = status.state === 'error' && status.error

  return <aside className="updater-notice" role="status" aria-live="polite">
    <div className="updater-notice-copy">
      {installed ? <Check size={17} aria-hidden="true"/> : <Download size={17} aria-hidden="true"/>}
      <div><strong>{installed ? '更新已下载' : `发现新版本 ${status.version}`}</strong><span>{installed ? '重启应用以完成更新。' : '可在方便时下载并安装。'}</span>{failed && <small role="alert">更新未安装：{status.error}</small>}{message && <small role="alert">{message}</small>}</div>
    </div>
    {downloading && <div className="updater-notice-progress"><progress max="1" value={status.progress}/><span>{Math.round(status.progress * 100)}%</span></div>}
    <div className="updater-notice-actions">
      {installed ? <button type="button" className="button button-primary" disabled={pending} onClick={() => void act('Updater.Relaunch')}><RotateCcw size={14}/>重启完成更新</button> : <button type="button" className="button button-primary" disabled={pending} onClick={() => void act('Updater.Install')}><Download size={14}/>{downloading ? '正在下载…' : '下载并安装'}</button>}
      {!installed && <><button type="button" className="button button-secondary" disabled={pending} onClick={() => setHidden(true)}>稍后</button><button type="button" className="icon-button" title="跳过此版本" aria-label="跳过此版本" disabled={pending} onClick={() => void act('Updater.Skip', status.version)}><SkipForward size={15}/></button></>}
      <button type="button" className="icon-button" title="关闭更新提示" aria-label="关闭更新提示" disabled={pending} onClick={() => setHidden(true)}><X size={15}/></button>
    </div>
  </aside>
}
