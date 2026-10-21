import { Check, ChevronRight, CircleAlert, Cloud, FolderOpen, HardDrive, History, KeyRound, Play, RefreshCw, RotateCcw, Settings2, ShieldCheck, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { APIError, request } from '../../shared/api/client'
import type { BackupRestoreResult, BackupRunErrorResponse, BackupSnapshot, BackupTarget, BackupTargetType, BackupTask, BackupValidation } from '../../shared/api/types'
import { PathInput } from './PathInput'
import { BackupRemoteFolderBrowser } from './BackupRemoteFolderBrowser'
import { BACKUP_SCOPE_NOTICE, backupEmptyState, backupStatusPresentation, backupValidationPresentation, formatBackupDate, formatBackupSize, immediateBackupAvailability } from './backupPresentation'
import { normalizeBackupSettingsData } from './backupFolderBrowser'
import { createRestoreFlow, restoreFlowCanCancel, restoreFlowCanConfirm, restoreFlowCanValidate, restoreFlowNeedsRestart, restoreFlowTransition, restoreStepPresentation, type RestoreFlowState } from './backupFlow'

const targetLabels: Record<BackupTargetType, string> = { local: '本地目录', webdav: 'WebDAV', s3: 'S3 兼容' }
const snapshotDirectory = 'tangentry/v1/snapshots'
type BackupPanel = 'backup' | 'history' | 'restore' | 'targets'

function newID(prefix: string) { return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}` }

export function backupDestination(target: BackupTarget): string {
  if (target.type === 'webdav' && target.endpoint) {
    try {
      const url = new URL(target.endpoint)
      const path = [url.pathname, target.remotePath].filter(Boolean).join('/').split('/').filter(Boolean)
      url.pathname = `/${[...path, snapshotDirectory].join('/')}`
      url.search = ''
      url.hash = ''
      return url.toString().replace(/\/$/, '')
    } catch { return `${target.endpoint.replace(/\/$/, '')}/${[target.remotePath, snapshotDirectory].filter(Boolean).join('/')}` }
  }
  if (target.type === 'local' && target.path) return `${target.path.replace(/\/$/, '')}/${snapshotDirectory}`
  return snapshotDirectory
}

export function BackupSettings() {
  const [panel, setPanel] = useState<BackupPanel>('backup')
  const [targets, setTargets] = useState<BackupTarget[]>([])
  const [tasks, setTasks] = useState<BackupTask[]>([])
  const [snapshots, setSnapshots] = useState<BackupSnapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [targetType, setTargetType] = useState<BackupTargetType>('local')
  const [targetName, setTargetName] = useState('')
  const [targetPath, setTargetPath] = useState('')
  const [targetEndpoint, setTargetEndpoint] = useState('')
  const [targetUsername, setTargetUsername] = useState('')
  const [targetPassword, setTargetPassword] = useState('')
  const [targetRemotePath, setTargetRemotePath] = useState('')
  const [allowInsecureTLS, setAllowInsecureTLS] = useState(false)
  const [targetTest, setTargetTest] = useState<'idle' | 'testing' | 'success' | 'error'>('idle')
  const [folderBrowserOpen, setFolderBrowserOpen] = useState(false)
  const [selectedTaskID, setSelectedTaskID] = useState('')
  const [selectedTargetIDs, setSelectedTargetIDs] = useState<string[]>([])
  const [editingTarget, setEditingTarget] = useState<string | null>(null)
  const [savingTarget, setSavingTarget] = useState(false)
  const [password, setPassword] = useState('')
  const [encryptionPassword, setEncryptionPassword] = useState('')
  const [encryptionPasswordConfirm, setEncryptionPasswordConfirm] = useState('')
  const [encryptionConfigured, setEncryptionConfigured] = useState(false)
  const [savingEncryption, setSavingEncryption] = useState(false)
  const [intervalHours, setIntervalHours] = useState(6)
  const [retention, setRetention] = useState(10)
  const [targetIntervals, setTargetIntervals] = useState<Record<string, number>>({})
  const [savingPlan, setSavingPlan] = useState(false)
  const [includeDownloads, setIncludeDownloads] = useState(false)
  const [backupState, setBackupState] = useState<'idle' | 'running' | 'success' | 'error'>('idle')
  const [backupResult, setBackupResult] = useState<BackupSnapshot | null>(null)
  const [retryingID, setRetryingID] = useState<string | null>(null)
  const [validatingID, setValidatingID] = useState<string | null>(null)
  const [validationBySnapshot, setValidationBySnapshot] = useState<Record<string, BackupValidation | null>>({})
  const [restore, setRestore] = useState<RestoreFlowState>(createRestoreFlow)
  const [restoreTargetID, setRestoreTargetID] = useState('')
  const [restorePassword, setRestorePassword] = useState('')
  const [restoreBusy, setRestoreBusy] = useState(false)
  const [restoreNotice, setRestoreNotice] = useState('')

  async function reload() {
    setLoading(true); setError('')
    try {
      const [loadedTargets, loadedTasks, loadedSnapshots, encryption] = await Promise.all([
        request<BackupTarget[]>('/backup/targets'), request<BackupTask[]>('/backup/tasks'), request<BackupSnapshot[]>('/backup/snapshots'), request<{ configured: boolean }>('/backup/encryption'),
      ])
      const safeTargets = normalizeBackupSettingsData(loadedTargets)
      const safeTasks = normalizeBackupSettingsData(loadedTasks)
      setTargets(safeTargets)
      setTasks(safeTasks.slice(0, 1))
      setSnapshots(normalizeBackupSettingsData(loadedSnapshots))
      setEncryptionConfigured(encryption.configured)
      if (!restoreTargetID && safeTargets[0]) setRestoreTargetID(safeTargets[0].id)
      const activeLoadedTask = safeTasks.find(task => task.id === selectedTaskID) ?? safeTasks[0]
      if (activeLoadedTask && (!selectedTaskID || !safeTasks.some(task => task.id === selectedTaskID))) {
        setSelectedTaskID(activeLoadedTask.id)
        setSelectedTargetIDs(activeLoadedTask.targetIds ?? [])
        setIncludeDownloads(Boolean(activeLoadedTask.includeDownloads))
        setIntervalHours(activeLoadedTask.intervalHours || 6)
        setRetention(activeLoadedTask.retention || 10)
        setTargetIntervals(activeLoadedTask.targetIntervalHours ?? {})
      } else if (!activeLoadedTask && !selectedTaskID) {
        setSelectedTargetIDs(safeTargets.length ? [safeTargets[0].id] : [])
        setIncludeDownloads(false)
      }
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份设置暂时无法读取，请重试。') }
    finally { setLoading(false) }
  }
  useEffect(() => { void reload() }, [])

  const activeTask = tasks[0]
  const selectedTarget = targets.find(target => target.id === restoreTargetID) ?? targets[0]
  const backupTargets = targets.filter(target => selectedTargetIDs.includes(target.id))
  const passwordError = encryptionPassword.length > 0 && encryptionPassword !== encryptionPasswordConfirm ? '两次输入的密码不一致。' : ''
  const runAvailability = immediateBackupAvailability({ hasTarget: selectedTargetIDs.length > 0, targetCount: selectedTargetIDs.length, hasPassword: encryptionConfigured, pendingAction: backupState === 'running' ? 'backup' : null })
  const restoreValidation = restore.validation ? backupValidationPresentation(restore.validation) : null

  function currentPlan(): BackupTask {
    return {
      id: activeTask?.id ?? newID('task'), name: '自动备份', targetIds: selectedTargetIDs,
      includeDownloads, retention, intervalHours,
      targetIntervalHours: Object.fromEntries(Object.entries(targetIntervals).filter(([id, hours]) => selectedTargetIDs.includes(id) && hours > 0)), enabled: true,
    }
  }

  async function savePlan() {
    if (savingPlan || selectedTargetIDs.length === 0) return
    setSavingPlan(true); setError('')
    const task = currentPlan()
    try {
      await request('/backup/tasks', { method: 'POST', body: JSON.stringify(task) })
      setTasks([task]); setSelectedTaskID(task.id)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份计划保存失败，请重试。') }
    finally { setSavingPlan(false) }
  }

  async function saveEncryption() {
    if (savingEncryption || encryptionPassword.length < 8 || encryptionPassword !== encryptionPasswordConfirm) return
    setSavingEncryption(true); setError('')
    try {
      await request('/backup/encryption', { method: 'PUT', body: JSON.stringify({ password: encryptionPassword }) })
      setEncryptionConfigured(true); setEncryptionPassword(''); setEncryptionPasswordConfirm('')
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份密码保存失败，请重试。') }
    finally { setSavingEncryption(false) }
  }

  function resetTargetForm() { setEditingTarget(null); setTargetName(''); setTargetPath(''); setTargetEndpoint(''); setTargetUsername(''); setTargetPassword(''); setTargetRemotePath(''); setAllowInsecureTLS(false); setTargetTest('idle'); setFolderBrowserOpen(false); setTargetType('local') }
  function editTarget(target: BackupTarget) { setEditingTarget(target.id); setTargetName(target.name); setTargetPath(target.path ?? ''); setTargetEndpoint(target.endpoint ?? ''); setTargetUsername(target.username ?? ''); setTargetPassword(''); setTargetRemotePath(target.remotePath ?? ''); setAllowInsecureTLS(Boolean(target.allowInsecureTLS)); setTargetTest('idle'); setFolderBrowserOpen(false); setTargetType(target.type === 'webdav' || target.type === 's3' ? target.type : 'local') }
  async function testTarget() {
    if (targetType !== 'webdav' || !targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured) return
    setTargetTest('testing'); setError('')
    try { await request('/backup/targets/test', { method: 'POST', body: JSON.stringify({ id: editingTarget ?? '', type: 'webdav', endpoint: targetEndpoint.trim(), username: targetUsername.trim(), password: targetPassword, remotePath: targetRemotePath.trim(), allowInsecureTLS }) }); setTargetTest('success'); setFolderBrowserOpen(true) }
    catch { setTargetTest('error'); setError('WebDAV 连接测试失败，请检查地址、账号、密码和网络。') }
  }
  async function saveTarget() {
    if (!targetName.trim() || targetType === 'local' && !targetPath.trim() || targetType === 'webdav' && (!targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured)) return
    setSavingTarget(true); setError('')
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 15000)
    try {
      const target: BackupTarget = { id: editingTarget ?? newID('target'), name: targetName.trim(), type: targetType, path: targetType === 'local' ? targetPath.trim() : undefined, endpoint: targetType === 'webdav' ? targetEndpoint.trim() : undefined, username: targetType === 'webdav' ? targetUsername.trim() : undefined, remotePath: targetType === 'webdav' ? targetRemotePath.trim() : undefined, allowInsecureTLS: targetType === 'webdav' ? allowInsecureTLS : undefined, password: targetType === 'webdav' ? targetPassword : undefined, updateCredential: targetType === 'webdav' && Boolean(targetPassword), enabled: true }
      await request('/backup/targets', { method: 'POST', body: JSON.stringify(target), signal: controller.signal })
      // Keep only the redacted target shape in UI state; the password is sent
      // once to the credential namespace and must not linger in target data.
      const redactedTarget: BackupTarget = { ...target, password: undefined, updateCredential: undefined, credentialConfigured: targetType === 'webdav' ? true : undefined }
      setTargets(current => editingTarget ? current.map(item => item.id === editingTarget ? { ...item, ...redactedTarget } : item) : [...current, redactedTarget])
      if (!restoreTargetID) setRestoreTargetID(target.id)
      resetTargetForm()
    } catch (cause) {
      setError(controller.signal.aborted ? '保存目标超时，请检查本地数据库状态后重试。' : cause instanceof Error ? cause.message : '备份目标保存失败，请检查设置。')
    } finally { window.clearTimeout(timeout); setSavingTarget(false) }
  }
  async function deleteTarget(target: BackupTarget) {
    setError('')
    try { await request(`/backup/targets/${encodeURIComponent(target.id)}`, { method: 'DELETE' }); setTargets(current => current.filter(item => item.id !== target.id)); setSelectedTargetIDs(current => current.filter(id => id !== target.id)); setTasks(current => current.map(task => ({ ...task, targetIds: task.targetIds.filter(id => id !== target.id) }))); if (restoreTargetID === target.id) setRestoreTargetID('') }
    catch (cause) { setError(cause instanceof Error ? cause.message : '备份目标删除失败，请重试。') }
  }
  async function runBackup() {
    if (runAvailability.disabled || !encryptionConfigured) return
    const knownSnapshotIDs = new Set(snapshots.map(snapshot => snapshot.id))
    setBackupState('running'); setBackupResult(null); setError('')
    try {
      const task = currentPlan()
      await request('/backup/tasks', { method: 'POST', body: JSON.stringify(task) })
      setTasks([task]); setSelectedTaskID(task.id)
      const result = await request<BackupSnapshot>(`/backup/tasks/${encodeURIComponent(task.id)}/run`, { method: 'POST', body: JSON.stringify({}) })
      setBackupResult(result); setSnapshots(current => [result, ...current.filter(snapshot => snapshot.id !== result.id)]); setBackupState('success')
    } catch (cause) {
      setBackupState('error')
      const failedSnapshot = cause instanceof APIError ? (cause.payload as BackupRunErrorResponse | undefined)?.snapshot : undefined
      if (failedSnapshot) {
        setBackupResult(failedSnapshot)
        setSnapshots(current => [failedSnapshot, ...current.filter(snapshot => snapshot.id !== failedSnapshot.id)])
      }
      try {
        const latest = normalizeBackupSettingsData(await request<BackupSnapshot[]>('/backup/snapshots'))
        setSnapshots(latest)
        const failedRun = latest.find(snapshot => !knownSnapshotIDs.has(snapshot.id))
        if (failedRun) setBackupResult(failedRun)
      } catch { /* Keep the original backup error visible if history is unavailable. */ }
      setError(cause instanceof Error ? cause.message : '备份失败，请检查目标和密码后重试。')
    }
  }
  async function validateSnapshot(snapshotID: string, targetID = selectedTarget?.id) {
    if (!targetID || !password.trim()) return
    setValidatingID(snapshotID); setError('')
    try { const result = await request<BackupValidation>('/backup/validate', { method: 'POST', body: JSON.stringify({ targetId: targetID, snapshotId: snapshotID, password }) }); setValidationBySnapshot(current => ({ ...current, [snapshotID]: result })); return result }
    catch (cause) { setValidationBySnapshot(current => ({ ...current, [snapshotID]: null })); setError(cause instanceof Error ? cause.message : '快照校验失败，请检查密码和备份目标。'); return null }
    finally { setValidatingID(null) }
  }
  async function retryFailed(snapshotID: string) {
    if (retryingID) return
    setRetryingID(snapshotID); setError('')
    try {
      const result = await request<BackupSnapshot>(`/backup/snapshots/${encodeURIComponent(snapshotID)}/retry-failed`, { method: 'POST' })
      setSnapshots(current => [result, ...current.filter(item => item.id !== result.id)])
      setBackupResult(current => current?.id === result.id ? result : current)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '失败目标重试失败，请稍后再试。')
    } finally { setRetryingID(null) }
  }
  function chooseRestoreSnapshot(snapshotID: string) { setRestore(restoreFlowTransition(createRestoreFlow(), { type: 'select-snapshot', snapshotId: snapshotID })); setRestorePassword(''); setRestoreNotice('') }
  function submitRestorePassword() {
    const withPassword = restoreFlowTransition(restore, { type: 'set-password', hasPassword: Boolean(restorePassword.trim()) })
    setRestore(restoreFlowTransition(withPassword, { type: 'submit-password' }))
  }
  async function preflightRestore() {
    if (!restoreFlowCanValidate(restore) || !restoreTargetID) return
    const validating = restoreFlowTransition(restore, { type: 'start-validation' })
    setRestore(validating)
    setRestoreBusy(true)
    try {
      const result = await request<BackupValidation>('/backup/validate', { method: 'POST', body: JSON.stringify({ targetId: restoreTargetID, snapshotId: restore.selectedSnapshotId, password: restorePassword }) })
      setValidationBySnapshot(current => ({ ...current, [restore.selectedSnapshotId]: result }))
      setRestore(restoreFlowTransition(validating, { type: 'validation-succeeded', validation: result }))
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : '快照校验失败，请检查密码和备份目标。'
      setError(message)
      setRestore(restoreFlowTransition(validating, { type: 'validation-failed', message }))
    } finally { setRestoreBusy(false) }
  }
  async function confirmRestore() {
    if (!restoreFlowCanConfirm(restore) || !restoreTargetID || !restorePassword.trim()) return
    const confirming = restoreFlowTransition(restore, { type: 'start-confirmation' })
    if (confirming.status !== 'confirming') {
      setRestore(confirming)
      return
    }
    setRestore(confirming)
    setRestoreBusy(true); setRestoreNotice('')
    try {
      const prepared = await request<BackupRestoreResult>('/backup/restore', { method: 'POST', body: JSON.stringify({ targetId: restoreTargetID, snapshotId: restore.selectedSnapshotId, password: restorePassword }) })
      const confirmed = await request<BackupRestoreResult>(`/backup/restore/${encodeURIComponent(prepared.planId)}/confirm`, { method: 'POST' })
      setRestore(restoreFlowTransition(confirming, { type: 'confirmation-succeeded', planId: confirmed.planId || prepared.planId })); setRestoreNotice('恢复计划已确认，需要重启后生效。')
    } catch (cause) { setRestore(restoreFlowTransition(confirming, { type: 'confirmation-failed', message: cause instanceof Error ? cause.message : '恢复确认失败，请重试。' })) }
    finally { setRestoreBusy(false) }
  }
  async function cancelRestore() {
    if (!restoreFlowCanCancel(restore) || !restore.planId) return
    setRestoreBusy(true)
    try { await request(`/backup/restore/${encodeURIComponent(restore.planId)}`, { method: 'DELETE' }); setRestore(current => restoreFlowTransition(current, { type: 'cancel-pending' })); setRestoreNotice('已取消待应用的恢复计划。') }
    catch (cause) { setRestoreNotice(cause instanceof Error ? cause.message : '取消恢复计划失败，请重试。') }
    finally { setRestoreBusy(false) }
  }

  return <div className="backup-settings">
    <nav className="backup-tabs" aria-label="备份与恢复页面">
      {([
        ['backup', '备份', <Play size={15}/>],
        ['history', '历史', <History size={15}/>],
        ['restore', '恢复', <RotateCcw size={15}/>],
        ['targets', '目标', <Settings2 size={15}/>],
      ] as const).map(([id, label, Icon]) => <button type="button" key={id} className={panel === id ? 'active' : ''} aria-current={panel === id ? 'page' : undefined} onClick={() => setPanel(id)}>{Icon}{label}{id === 'history' && snapshots.length > 0 && <span>{snapshots.length}</span>}</button>)}
      <button type="button" className="backup-refresh icon-button" aria-label="刷新备份状态" title="刷新备份状态" onClick={() => void reload()} disabled={loading}><RefreshCw size={16} className={loading ? 'spin' : undefined}/></button>
    </nav>
    {error && <p className="subtitle-error" role="alert">{error}</p>}

    {panel === 'backup' && <section className="backup-panel" aria-labelledby="backup-run-title">
      <div className="backup-panel-heading"><div><h2 id="backup-run-title">创建备份与计划</h2><p>应用运行时按计划保存快照，也可以随时手动执行。</p></div>{snapshots[0] && <div className="backup-latest"><span>最近一次</span><strong>{formatBackupDate(snapshots[0].createdAt)}</strong><span className={`backup-status backup-status-${backupStatusPresentation(snapshots[0].status).tone}`}>{backupStatusPresentation(snapshots[0].status).label}</span></div>}</div>
      {!targets.length && <div className="backup-empty"><HardDrive size={20}/><div><strong>还没有备份目标</strong><p>先添加一个本地目录或 WebDAV 位置。</p><button type="button" className="text-button" onClick={() => setPanel('targets')}>添加目标</button></div></div>}
      <div className="backup-run-form">
        <div className="backup-plan-settings"><label>默认备份间隔（小时）<input aria-label="默认备份间隔小时" type="number" min={1} max={720} value={intervalHours} onChange={event => setIntervalHours(Math.max(1, Math.min(720, Number(event.target.value) || 1)))}/></label><label>每个位置最多保留<input type="number" min={1} max={1000} value={retention} onChange={event => setRetention(Math.max(1, Math.min(1000, Number(event.target.value) || 1)))}/></label><span className="field-hint">自动备份仅在应用运行时执行；电脑唤醒后会补跑逾期目标。</span></div>
        <div className="backup-password-grid"><label><span><KeyRound size={14}/>备份文件加密密码</span><input id="backup-password" type="password" autoComplete="new-password" value={encryptionPassword} onChange={event => setEncryptionPassword(event.target.value)} placeholder={encryptionConfigured ? '已安全保存；输入新密码可更换' : '至少 8 个字符'}/></label><label><span>确认加密密码</span><input type="password" autoComplete="new-password" value={encryptionPasswordConfirm} onChange={event => setEncryptionPasswordConfirm(event.target.value)} placeholder="再次输入密码"/></label></div>
        <div className="backup-encryption-actions"><p className="field-hint backup-warning"><CircleAlert size={14}/>这是快照加密密码，不是 WebDAV 登录密码。忘记后无法恢复。</p><button type="button" className="button button-secondary" disabled={savingEncryption || encryptionPassword.length < 8 || encryptionPassword !== encryptionPasswordConfirm} onClick={() => void saveEncryption()}>{savingEncryption ? '正在安全保存…' : encryptionConfigured ? '更换密码' : '保存加密密码'}</button></div>{passwordError && <p className="subtitle-error" role="alert">{passwordError}</p>}
        <label className="backup-history-password">用于校验旧快照的密码<input type="password" autoComplete="off" value={password} onChange={event => setPassword(event.target.value)} placeholder="重新输入创建旧快照时使用的密码"/></label>
        <fieldset className="backup-target-checkboxes"><legend>保存位置与频率</legend>{targets.length === 0 ? <span>请先添加备份目标。</span> : targets.map(target => <div className="backup-plan-target" key={target.id}><label><input type="checkbox" checked={selectedTargetIDs.includes(target.id)} onChange={event => setSelectedTargetIDs(current => event.target.checked ? [...current, target.id] : current.filter(id => id !== target.id))}/>{target.name}</label><label className="backup-target-interval"><span>间隔小时</span><input aria-label={`${target.name}备份间隔小时`} type="number" min={1} max={720} placeholder={`默认 ${intervalHours}`} disabled={!selectedTargetIDs.includes(target.id)} value={targetIntervals[target.id] || ''} onChange={event => setTargetIntervals(current => ({ ...current, [target.id]: event.target.value ? Number(event.target.value) : 0 }))}/></label></div>)}</fieldset>
        {backupTargets.length > 0 && <div className="backup-destinations"><span>快照将写入</span>{backupTargets.map(target => <div key={target.id}><strong>{target.name}</strong><code>{backupDestination(target)}</code></div>)}</div>}
        <label className="backup-checkbox"><input type="checkbox" checked={includeDownloads} onChange={event => setIncludeDownloads(event.target.checked)}/>包含下载媒体（默认关闭）</label><p className="field-hint">{BACKUP_SCOPE_NOTICE}</p>
        {!encryptionConfigured && <p className="field-hint" role="status">请先保存备份加密密码，之后定时与手动备份都会使用它。</p>}
        <div className="backup-run-actions"><button type="button" className="button button-secondary" disabled={savingPlan || selectedTargetIDs.length === 0 || backupState === 'running'} onClick={() => void savePlan()}><Check size={15}/>{savingPlan ? '正在保存…' : '保存计划'}</button><button type="button" className="button button-primary" disabled={runAvailability.disabled || !encryptionConfigured || savingPlan} onClick={() => void runBackup()}><Play size={15}/>{backupState === 'running' ? '正在生成、加密并上传…' : '立即备份'}</button>{selectedTargetIDs.length === 0 && <span className="field-hint" role="status">请至少选择一个备份目标</span>}{runAvailability.disabled && selectedTargetIDs.length > 0 && <span className="field-hint" role="status">{runAvailability.reason}</span>}</div>
      </div>
      {backupResult && <div className={`backup-run-result backup-result-${backupStatusPresentation(backupResult.status).tone}`} role="status"><div><strong>{backupStatusPresentation(backupResult.status).label}</strong><span>{formatBackupDate(backupResult.createdAt)} · {formatBackupSize(backupResult.size)}</span></div><code>{backupResult.id}.tgb</code><div className="backup-result-targets">{backupResult.targets?.map(target => <div key={target.targetId}><span className={`backup-status backup-status-${target.status === 'succeeded' ? 'success' : target.status === 'failed' ? 'danger' : 'neutral'}`}>{target.status === 'succeeded' ? '已上传' : target.status === 'failed' ? '上传失败' : target.status}</span><strong>{target.name || target.targetId}</strong>{target.status === 'failed' && target.error && <span>{target.error}</span>}</div>)}</div><p>远端文件位于所选目录下的 <code>{snapshotDirectory}/{backupResult.id}.tgb</code></p></div>}
    </section>}

    {panel === 'targets' && <section className="backup-panel" aria-labelledby="backup-targets-title">
      <div className="backup-panel-heading"><div><h2 id="backup-targets-title">备份目标</h2><p>每个快照会保存到所选目标下的固定目录。</p></div></div>
      {targets.length === 0 && <div className="backup-empty"><HardDrive size={20}/><div><strong>尚未配置目标</strong><p>添加本地目录或 WebDAV 位置后即可开始备份。</p></div></div>}
      <div className="backup-target-list">{targets.map(target => <div className="backup-target-row" key={target.id}><span className="backup-target-icon">{target.type === 'local' ? <HardDrive size={17}/> : <Cloud size={17}/>}</span><div><strong>{target.name}</strong><p>{targetLabels[target.type as BackupTargetType] ?? target.type} · {target.type === 'webdav' ? `${target.endpoint}${target.remotePath ? ` / ${target.remotePath}` : ''}` : target.path || '未填写位置'}</p><p className="backup-destination-path">快照目录：{backupDestination(target)}</p>{target.type === 'webdav' && <p className="field-hint">{target.credentialConfigured ? '凭据已配置' : '尚未配置凭据'}</p>}</div><div className="backup-row-actions"><button type="button" className="text-button" onClick={() => editTarget(target)}>编辑</button><button type="button" className="icon-button" aria-label={`删除${target.name}`} title="删除目标" onClick={() => void deleteTarget(target)}><Trash2 size={15}/></button></div></div>)}</div>
      <div className="backup-target-editor"><h3>{editingTarget ? '编辑备份目标' : '添加备份目标'}</h3><div className="backup-form-grid"><label>目标类型<select value={targetType} onChange={event => { setTargetType(event.target.value as BackupTargetType); setFolderBrowserOpen(false) }}><option value="local">本地目录</option><option value="webdav">WebDAV</option><option value="s3" disabled>S3 兼容（即将支持）</option></select></label><label>名称<input id="backup-target-name" value={targetName} maxLength={120} placeholder="例如：家庭 NAS" onChange={event => setTargetName(event.target.value)}/></label>{targetType === 'local' ? <div className="backup-form-wide"><PathInput label="目录" ariaLabel="备份目录" value={targetPath} kind="backup-directory" placeholder="填写 Tangentry 服务端上的目录" onChange={setTargetPath}/></div> : targetType === 'webdav' ? <><label className="backup-form-wide">WebDAV 地址<input value={targetEndpoint} placeholder="https://nas.example/dav" onChange={event => { setTargetEndpoint(event.target.value); setTargetTest('idle'); setFolderBrowserOpen(false) }}/></label><label>用户名<input value={targetUsername} autoComplete="username" onChange={event => { setTargetUsername(event.target.value); setTargetTest('idle'); setFolderBrowserOpen(false) }}/></label><label>密码<input type="password" value={targetPassword} autoComplete="new-password" placeholder={editingTarget ? '留空以保留当前密码' : '输入 WebDAV 密码'} onChange={event => { setTargetPassword(event.target.value); setFolderBrowserOpen(false) }}/></label><div className="backup-form-wide backup-remote-path"><label htmlFor="backup-remote-path">远端目录</label><div><input id="backup-remote-path" value={targetRemotePath || '根目录'} readOnly aria-readonly="true"/><button type="button" className="button button-secondary" disabled={!targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured} onClick={() => setFolderBrowserOpen(value => !value)}><FolderOpen size={16}/>浏览…</button></div></div><label className="backup-checkbox backup-form-wide"><input type="checkbox" checked={allowInsecureTLS} onChange={event => { setAllowInsecureTLS(event.target.checked); setFolderBrowserOpen(false) }}/>允许自签名证书</label><div className="backup-editor-actions backup-form-wide"><button type="button" className="button button-secondary" disabled={targetTest === 'testing' || !targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured} onClick={() => void testTarget()}>测试连接</button>{targetTest === 'success' && <span className="backup-success">连接成功</span>}{targetTest === 'error' && <span className="subtitle-error">连接失败</span>}</div>{folderBrowserOpen && <div className="backup-form-wide"><BackupRemoteFolderBrowser target={{ id: editingTarget ?? '', type: 'webdav', endpoint: targetEndpoint.trim(), username: targetUsername.trim(), remotePath: targetRemotePath.trim(), password: targetPassword, allowInsecureTLS }} onSelect={path => { setTargetRemotePath(path); setFolderBrowserOpen(false) }} onClose={() => setFolderBrowserOpen(false)}/></div>}</> : <p className="field-hint backup-form-wide">S3 兼容目标即将支持。</p>}</div><div className="backup-editor-actions"><button type="button" className="button button-secondary" disabled={savingTarget || targetType === 's3' || !targetName.trim() || targetType === 'local' && !targetPath.trim() || targetType === 'webdav' && (!targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured)} onClick={() => void saveTarget()}>{savingTarget ? '正在保存…' : editingTarget ? '保存目标' : '添加目标'}</button>{editingTarget && <button type="button" className="text-button" onClick={resetTargetForm}>取消编辑</button>}</div></div>
    </section>}

    {panel === 'history' && <section className="backup-panel" aria-labelledby="backup-snapshots-title"><div className="backup-panel-heading"><div><h2 id="backup-snapshots-title">快照历史</h2><p>确认具体目标的上传状态，再从相同目标校验或恢复。</p></div></div>{snapshots.length === 0 ? <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p><button type="button" className="text-button" onClick={() => setPanel('backup')}>创建备份</button></div></div> : <div className="backup-snapshot-list">{snapshots.map(snapshot => { const status = backupStatusPresentation(snapshot.status); const hasValidation = Object.prototype.hasOwnProperty.call(validationBySnapshot, snapshot.id); const validation = validationBySnapshot[snapshot.id]; const details = validation ? backupValidationPresentation(validation) : null; const retryDisabled = !snapshot.retryAvailable; const validationTargetID = snapshot.targets?.find(target => target.status === 'succeeded')?.targetId ?? selectedTarget?.id; return <article className="backup-snapshot-row" key={snapshot.id}><div className="backup-snapshot-main"><div className="backup-snapshot-title"><strong>{formatBackupDate(snapshot.createdAt)}</strong><span className={`backup-status backup-status-${status.tone}`}>{status.label}</span><span>{formatBackupSize(snapshot.size)}</span></div><small>{snapshot.id}.tgb</small><div className="backup-result-targets">{snapshot.targets?.map(target => <div key={target.targetId}><span className={`backup-status backup-status-${target.status === 'succeeded' ? 'success' : target.status === 'failed' ? 'danger' : 'neutral'}`}>{target.status === 'succeeded' ? '已上传' : target.status === 'failed' ? '失败' : target.status}</span><strong>{target.name || target.targetId}</strong>{target.status === 'failed' && target.error && <span>{target.error}</span>}</div>)}</div>{retryDisabled && snapshot.retryUnavailableReason && <span className="field-hint" role="status">{snapshot.retryUnavailableReason}</span>}{hasValidation && <span className={`backup-status backup-status-${validation ? 'success' : 'danger'}`}>{validation ? '校验通过' : '校验失败'}</span>}{details && <details className="backup-validation-details"><summary>查看文件校验</summary>{details.files.map(file => <div key={file.path}><code>{file.path}</code><span>{file.size} · SHA-256 {file.sha256}</span></div>)}</details>}</div><div className="backup-snapshot-actions">{snapshot.status !== 'succeeded' && <button type="button" className="button button-secondary" disabled={retryingID !== null || retryDisabled} title={retryDisabled ? snapshot.retryUnavailableReason : '仅重试失败目标'} onClick={() => void retryFailed(snapshot.id)}><RefreshCw size={14} className={retryingID === snapshot.id ? 'spin' : undefined}/>{retryingID === snapshot.id ? '重试中…' : '重试失败目标'}</button>}<button type="button" className="button button-secondary" disabled={validatingID === snapshot.id || !password.trim() || !validationTargetID} onClick={() => void validateSnapshot(snapshot.id, validationTargetID)}>{validatingID === snapshot.id ? '校验中…' : hasValidation ? '重新校验' : '校验'}</button></div></article> })}</div>}</section>}

    {panel === 'restore' && <section className="backup-panel" aria-labelledby="backup-restore-title"><div className="backup-panel-heading"><div><h2 id="backup-restore-title">恢复备份</h2><p>恢复前会预检快照；确认后需要重启应用才会生效。</p></div></div><div className="backup-stepper">{(['select-snapshot', 'enter-password', 'preflight', 'confirm'] as const).map((step, index) => { const presentation = restoreStepPresentation(step); return <div className={restore.step === step ? 'active' : ''} key={step}><span>{presentation.number}</span><small>{presentation.title}</small>{index < 3 && <ChevronRight size={14}/>}</div> })}</div>
      <div className="backup-restore-form"><label>恢复目标<select value={restoreTargetID} onChange={event => setRestoreTargetID(event.target.value)} disabled={!targets.length}><option value="">请选择目标</option>{targets.map(target => <option key={target.id} value={target.id}>{target.name}</option>)}</select></label><label>选择快照<select value={restore.selectedSnapshotId} onChange={event => chooseRestoreSnapshot(event.target.value)} disabled={!snapshots.length}><option value="">请选择快照</option>{snapshots.map(snapshot => <option key={snapshot.id} value={snapshot.id}>{formatBackupDate(snapshot.createdAt)} · {snapshot.id}</option>)}</select></label>{restore.step === 'enter-password' && <div className="backup-restore-step"><label>备份密码<input type="password" autoComplete="off" value={restorePassword} onChange={event => setRestorePassword(event.target.value)} placeholder="输入创建快照时设置的密码"/></label><button type="button" className="button button-secondary" disabled={!restorePassword.trim()} onClick={submitRestorePassword}>继续预检</button></div>}{restore.step === 'preflight' && <div className="backup-restore-step"><p>{restore.error || '确认密码后开始预检。'}</p><button type="button" className="button button-secondary" disabled={!restoreFlowCanValidate(restore) || restoreBusy} onClick={() => void preflightRestore()}>{restore.status === 'validating' ? '校验中…' : '开始预检'}</button></div>}{restoreValidation && <div className="backup-validation-panel"><strong>预检结果</strong><p>格式版本：v{restoreValidation.formatVersion ?? '?'}，数据库架构版本：v{restoreValidation.schemaVersion ?? '?'}，文件数量：{restoreValidation.fileCount}，大小：{restoreValidation.size}</p><p className="field-hint">{restoreValidation.available ? '可恢复性：通过' : '可恢复性：未知'}</p></div>}{restore.step === 'confirm' && <div className="backup-restore-step"><p>确认恢复后会写入恢复计划，重启后应用。运行中的数据不会热切换。</p><button type="button" className="button button-primary" disabled={!restoreFlowCanConfirm(restore) || restoreBusy} onClick={() => void confirmRestore()}>{restoreBusy ? '正在确认…' : '确认恢复'}</button></div>}{restoreFlowNeedsRestart(restore) && <div className="backup-pending-restore" role="status"><Check size={16}/><div><strong>恢复计划待应用</strong><p>需要重启后生效。计划标识：{restore.planId}</p></div><button type="button" className="button button-secondary" disabled={restoreBusy} onClick={() => void cancelRestore()}>取消待应用恢复</button></div>}{restore.error && restore.step !== 'preflight' && <p className="subtitle-error" role="alert">{restore.error}</p>}{restoreNotice && <p className="field-hint" role="status">{restoreNotice}</p>}{!snapshots.length && <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p></div></div>}</div>
    </section>}
    {loading && <p className="field-hint" role="status">正在读取备份状态…</p>}
  </div>
}
