import { Check, CircleHelp, Download, Folder, FolderOpen, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import { useCapabilities } from '../../shared/api/desktop'
import type { ToolStatus, BackupTarget, BackupTask, BackupSnapshot, BackupRestoreResult, BackupValidation } from '../../shared/api/types'
import type { useDownloads } from '../downloads/useDownloads'

function toolProgress(tool: ToolStatus) {
  return tool.total ? `正在下载 ${Math.floor((tool.received ?? 0) * 100 / tool.total)}%` : '正在下载…'
}

export function StorageSettings({ downloads, onModelDirectoryChanged }: {
  downloads: ReturnType<typeof useDownloads>
  onModelDirectoryChanged: () => void
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [directory, setDirectory] = useState<string | null>(null)
  const [modelDirectory, setModelDirectory] = useState('')
  const { pathPicker } = useCapabilities()
  const [updating, setUpdating] = useState(false), [updateError, setUpdateError] = useState('')
  const toolDownloading = downloads.tools?.tools.some(tool => tool.downloading) ?? false
  const [backupTargets, setBackupTargets] = useState<BackupTarget[]>([])
  const [backupTasks, setBackupTasks] = useState<BackupTask[]>([])
  const [backupSnapshots, setBackupSnapshots] = useState<BackupSnapshot[]>([])
  const [backupPath, setBackupPath] = useState('')
  const [backupError, setBackupError] = useState('')
  const [backupPassword, setBackupPassword] = useState('')
  const [restorePlan, setRestorePlan] = useState<BackupRestoreResult | null>(null)
  const [restoreValidation, setRestoreValidation] = useState<BackupValidation | null>(null)
  const [restoreStatus, setRestoreStatus] = useState('')
  const [selectedSnapshotId, setSelectedSnapshotId] = useState('')
  const { reload } = downloads
  useEffect(() => {
    if (!toolDownloading) return
    const timer = window.setInterval(() => { if (!document.hidden) void reload() }, 1000)
    return () => window.clearInterval(timer)
  }, [toolDownloading, reload])
  async function update() {
    setUpdating(true); setUpdateError('')
    try {
      await request('/tools/yt-dlp/update', { method: 'POST' })
      await reload()
    } catch (cause) {
      setUpdateError(cause instanceof Error ? cause.message : '无法开始更新。')
    } finally { setUpdating(false) }
  }
  useEffect(() => {
    if (!pathPicker) return
    const controller = new AbortController()
    void request<{ directory: string }>('/desktop/download-directory', { signal: controller.signal }).then(result => setDirectory(result.directory || '尚未选择')).catch(() => {})
    void request<{ directory: string }>('/desktop/model-directory', { signal: controller.signal }).then(result => setModelDirectory(result.directory)).catch(() => {})
    return () => controller.abort()
  }, [pathPicker])
  useEffect(() => {
    const controller = new AbortController()
    void Promise.all([
      request<BackupTarget[]>('/backup/targets', { signal: controller.signal }),
      request<BackupTask[]>('/backup/tasks', { signal: controller.signal }),
      request<BackupSnapshot[]>('/backup/snapshots', { signal: controller.signal }),
    ]).then(([targets, tasks, snapshots]) => { setBackupTargets(targets); setBackupTasks(tasks); setBackupSnapshots(snapshots); setSelectedSnapshotId(snapshots[0]?.id ?? '') }).catch(() => {})
    return () => controller.abort()
  }, [])
  async function addBackupTarget() {
    setBackupError('')
    try {
      if (!backupPath.trim()) throw new Error('请填写备份目录。')
      const target = { id: crypto.randomUUID(), name: '本地备份目录', type: 'local', path: backupPath.trim(), enabled: true }
      await request('/backup/targets', { method: 'POST', body: JSON.stringify(target) })
      setBackupTargets(current => [...current, target as BackupTarget]); setBackupPath('')
    } catch (cause) { setBackupError(cause instanceof Error ? cause.message : '无法添加备份目录。') }
  }
  async function createBackup() {
    setBackupError('')
    try {
      if (!backupTargets.length) throw new Error('请先添加备份目录。')
      let task = backupTasks[0]
      if (!task) {
        task = { id: crypto.randomUUID(), name: '手动备份', targetIds: backupTargets.map(target => target.id), includeDownloads: false, retention: 0, enabled: true }
        await request('/backup/tasks', { method: 'POST', body: JSON.stringify(task) }); setBackupTasks([task])
      }
      const snapshot = await request<BackupSnapshot>(`/backup/tasks/${task.id}/run`, { method: 'POST', body: JSON.stringify({ password: backupPassword }) })
      setBackupSnapshots(current => [snapshot, ...current]); setBackupPassword('')
    } catch (cause) { setBackupError(cause instanceof Error ? cause.message : '备份失败，请检查设置。') }
  }
  async function validateRestore(snapshotId: string) {
    setBackupError(''); setRestoreStatus('')
    try {
      const target = backupTargets[0]
      if (!target) throw new Error('请先设置备份目录。')
      const validation = await request<BackupValidation>('/backup/validate', { method: 'POST', body: JSON.stringify({ targetId: target.id, snapshotId, password: backupPassword }) })
      setRestoreValidation(validation)
      const plan = await request<BackupRestoreResult>('/backup/restore', { method: 'POST', body: JSON.stringify({ targetId: target.id, snapshotId, password: backupPassword }) })
      setRestorePlan(plan); setRestoreStatus(`预检通过，恢复计划 ${plan.planId} 已暂存。确认后将在重启应用时生效。` )
    } catch (cause) { setBackupError(cause instanceof Error ? cause.message : '恢复预检失败。') }
  }
  async function confirmRestore() {
    if (!restorePlan) return
    try { await request<BackupRestoreResult>(`/backup/restore/${restorePlan.planId}/confirm`, { method: 'POST' }); setRestoreStatus('已确认恢复，将在重启应用后生效；当前运行数据不会被热切换。'); setRestorePlan(null); setRestoreValidation(null) } catch (cause) { setBackupError(cause instanceof Error ? cause.message : '恢复确认失败。') }
  }
  async function chooseDirectory(models: boolean) {
    setPending(true)
    setError('')
    try {
      const result = await request<{ directory: string; cancelled: boolean }>(models ? '/desktop/model-directory' : '/desktop/download-directory', { method: 'POST' })
      if (result.cancelled) return
      if (models) {
        setModelDirectory(result.directory)
        onModelDirectoryChanged()
      } else {
        setDirectory(result.directory)
        await downloads.reload()
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : models ? '模型迁移失败。' : '无法选择下载目录。')
    } finally { setPending(false) }
  }
  return <>
    <section className="settings-section" aria-labelledby="storage-title">
      <div className="settings-heading"><div><h2 id="storage-title">文件位置</h2><p className="field-hint">管理下载文件与离线模型的保存位置。</p></div></div>
      <div className="settings-location-row">
        <Folder size={20} aria-hidden="true"/>
        <div><strong>下载目录</strong><p className="settings-location-path">{directory ?? downloads.tools?.downloadDir ?? '正在读取…'}</p></div>
        {pathPicker && <button type="button" className="button button-secondary" title="选择下载目录" aria-label="选择下载目录" disabled={pending} onClick={() => void chooseDirectory(false)}><FolderOpen size={15}/>更改</button>}
      </div>
      {pathPicker && <div className="settings-location-row">
        <Folder size={20} aria-hidden="true"/>
        <div><strong>模型存储目录</strong><p className="settings-location-path">{modelDirectory || '正在读取…'}</p><p className="field-hint">{pending ? '正在处理，请稍候…' : '更改位置时自动迁移已下载模型。'}</p></div>
        <button type="button" className="button button-secondary" title="选择并迁移模型目录" aria-label="选择并迁移模型目录" disabled={pending} onClick={() => void chooseDirectory(true)}><FolderOpen size={15}/>更改</button>
      </div>}
      {error && <p className="subtitle-error" role="alert">{error}</p>}
    </section>
    <section className="settings-section" aria-labelledby="tools-title">
      <div className="settings-heading"><div><h2 id="tools-title">下载工具</h2><p className="field-hint">更改系统工具后，请重启。</p></div><button className="icon-button" aria-label="刷新状态" title="刷新状态" disabled={downloads.loading} onClick={() => void downloads.reload()}><RefreshCw size={16} className={downloads.loading ? 'spin' : undefined}/></button></div>
      {downloads.loading && <p className="field-hint" role="status">正在检查工具…</p>}
      {downloads.tools?.tools.map(tool => <div className="setting-row settings-tool-row" key={tool.name}><div><strong>{tool.name}</strong><p>{tool.name === 'yt-dlp' ? '获取视频与已有字幕' : '合并音视频与媒体处理'}</p>{tool.managed && !tool.installed && !tool.downloading && !tool.error && <p className="field-hint">首次使用时自动下载并校验。</p>}{tool.error && <p className="subtitle-error" role="alert">{tool.error}</p>}</div><div className="tool-value">{tool.downloading ? <span className="settings-state" role="status"><RefreshCw size={14} className="spin"/> {toolProgress(tool)}</span> : <span className="settings-state" data-ready={tool.available}>{tool.available ? <Check size={14}/> : <CircleHelp size={14}/>} {tool.available ? '已就绪' : '未检测到'}</span>}{tool.version && <small>{tool.version.replace(/^ffmpeg version /, '').split(' Copyright')[0]}</small>}{tool.managed && <button type="button" className="button button-secondary" disabled={tool.downloading || updating} onClick={() => void update()}><Download size={14}/>{tool.installed ? '更新 yt-dlp' : '下载 yt-dlp'}</button>}</div></div>)}
      {updateError && <p className="subtitle-error" role="alert">{updateError}</p>}
    </section>
    <section className="settings-section" aria-labelledby="backup-title">
      <div className="settings-heading"><div><h2 id="backup-title">云端备份</h2><p className="field-hint">快照在本机加密后保存到本地目录。默认不包含下载媒体。</p></div></div>
      <div className="setting-row"><label htmlFor="backup-path"><strong>本地备份目录</strong><span className="field-hint">可选网络挂载目录</span></label><input id="backup-path" value={backupPath} onChange={event => setBackupPath(event.target.value)} placeholder="例如 /Volumes/Backup"/><button type="button" className="button button-secondary" onClick={() => void addBackupTarget()}>添加</button></div>
      {backupTargets.map(target => <div className="setting-row" key={target.id}><div><strong>{target.name}</strong><p className="settings-location-path">{target.path}</p></div><span className="settings-state" data-ready="true">已启用</span></div>)}
      <div className="setting-row"><label htmlFor="backup-password"><strong>备份密码</strong><span className="field-hint">不会写入数据库或快照</span></label><input id="backup-password" type="password" value={backupPassword} onChange={event => setBackupPassword(event.target.value)} autoComplete="new-password"/><button type="button" className="button button-primary" disabled={!backupPassword || !backupTargets.length} onClick={() => void createBackup()}>立即备份</button></div>
      {backupSnapshots.length > 0 && <div className="backup-history" aria-label="备份历史">{backupSnapshots.slice(0, 5).map(snapshot => <div className="setting-row" key={snapshot.id}><span>{new Date(snapshot.createdAt).toLocaleString('zh-CN')}</span><span>{Math.ceil(snapshot.size / 1024)} KB</span><span className="settings-state" data-ready={snapshot.status === 'succeeded'}>{snapshot.status === 'succeeded' ? '成功' : snapshot.status}</span></div>)}</div>}
      {backupSnapshots.length > 0 && <div className="setting-row"><label htmlFor="backup-snapshot"><strong>恢复快照</strong><span className="field-hint">先校验，再确认重启切换</span></label><select id="backup-snapshot" value={selectedSnapshotId} onChange={event => setSelectedSnapshotId(event.target.value)}>{backupSnapshots.map(snapshot => <option key={snapshot.id} value={snapshot.id}>{new Date(snapshot.createdAt).toLocaleString('zh-CN')} · {snapshot.id}</option>)}</select><button type="button" className="button button-secondary" disabled={!backupPassword || !selectedSnapshotId} onClick={() => void validateRestore(selectedSnapshotId)}>校验并准备恢复</button></div>}
      {restoreValidation && <div className="setting-row"><div><strong>校验结果</strong><span className="field-hint">格式 v{restoreValidation.manifest.formatVersion}，数据库架构 v{restoreValidation.manifest.schemaVersion}，{restoreValidation.manifest.files.length} 个文件，{Math.ceil(restoreValidation.size / 1024)} KB</span></div></div>}
      {restorePlan && <div className="setting-row"><span className="field-hint">{restoreStatus}</span><button type="button" className="button button-primary" onClick={() => void confirmRestore()}>确认恢复</button></div>}
      {restoreStatus && !restorePlan && <p className="field-hint" role="status">{restoreStatus}</p>}
      {backupError && <p className="subtitle-error" role="alert">{backupError}</p>}
    </section>
  </>
}
