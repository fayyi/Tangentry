import { Check, ChevronRight, CircleAlert, Cloud, FolderOpen, HardDrive, KeyRound, Play, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import type { BackupRestoreResult, BackupSnapshot, BackupTarget, BackupTargetType, BackupTask, BackupValidation } from '../../shared/api/types'
import { PathInput } from './PathInput'
import { BackupRemoteFolderBrowser } from './BackupRemoteFolderBrowser'
import { BACKUP_SCOPE_NOTICE, backupEmptyState, backupStatusPresentation, backupValidationPresentation, formatBackupDate, formatBackupSize, immediateBackupAvailability } from './backupPresentation'
import { normalizeBackupSettingsData } from './backupFolderBrowser'
import { createRestoreFlow, restoreFlowCanCancel, restoreFlowCanConfirm, restoreFlowCanValidate, restoreFlowNeedsRestart, restoreFlowTransition, restoreStepPresentation, type RestoreFlowState } from './backupFlow'

const targetLabels: Record<BackupTargetType, string> = { local: '本地目录', webdav: 'WebDAV', s3: 'S3 兼容' }

function newID(prefix: string) { return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}` }

export function BackupSettings() {
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
  const [passwordConfirm, setPasswordConfirm] = useState('')
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
      const [loadedTargets, loadedTasks, loadedSnapshots] = await Promise.all([
        request<BackupTarget[]>('/backup/targets'), request<BackupTask[]>('/backup/tasks'), request<BackupSnapshot[]>('/backup/snapshots'),
      ])
      const safeTargets = normalizeBackupSettingsData(loadedTargets)
      const safeTasks = normalizeBackupSettingsData(loadedTasks)
      setTargets(safeTargets)
      setTasks(safeTasks)
      setSnapshots(normalizeBackupSettingsData(loadedSnapshots))
      if (!restoreTargetID && safeTargets[0]) setRestoreTargetID(safeTargets[0].id)
      const activeLoadedTask = safeTasks.find(task => task.id === selectedTaskID) ?? safeTasks[0]
      if (activeLoadedTask && (!selectedTaskID || !safeTasks.some(task => task.id === selectedTaskID))) {
        setSelectedTaskID(activeLoadedTask.id)
        setSelectedTargetIDs(activeLoadedTask.targetIds ?? [])
        setIncludeDownloads(Boolean(activeLoadedTask.includeDownloads))
      } else if (!activeLoadedTask && !selectedTaskID) {
        setSelectedTargetIDs(safeTargets.length ? [safeTargets[0].id] : [])
        setIncludeDownloads(false)
      }
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份设置暂时无法读取，请重试。') }
    finally { setLoading(false) }
  }
  useEffect(() => { void reload() }, [])

  const activeTask = selectedTaskID ? tasks.find(task => task.id === selectedTaskID) : undefined
  const selectedTarget = targets.find(target => target.id === restoreTargetID) ?? targets[0]
  const passwordError = password.length > 0 && password !== passwordConfirm ? '两次输入的密码不一致。' : ''
  const runAvailability = immediateBackupAvailability({ hasTarget: selectedTargetIDs.length > 0, targetCount: selectedTargetIDs.length, password, pendingAction: backupState === 'running' ? 'backup' : null })
  const restoreValidation = restore.validation ? backupValidationPresentation(restore.validation) : null

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
    try {
      const target: BackupTarget = { id: editingTarget ?? newID('target'), name: targetName.trim(), type: targetType, path: targetType === 'local' ? targetPath.trim() : undefined, endpoint: targetType === 'webdav' ? targetEndpoint.trim() : undefined, username: targetType === 'webdav' ? targetUsername.trim() : undefined, remotePath: targetType === 'webdav' ? targetRemotePath.trim() : undefined, allowInsecureTLS: targetType === 'webdav' ? allowInsecureTLS : undefined, password: targetType === 'webdav' ? targetPassword : undefined, updateCredential: targetType === 'webdav' && Boolean(targetPassword), enabled: true }
      await request('/backup/targets', { method: 'POST', body: JSON.stringify(target) })
      // Keep only the redacted target shape in UI state; the password is sent
      // once to the credential namespace and must not linger in target data.
      const redactedTarget: BackupTarget = { ...target, password: undefined, updateCredential: undefined, credentialConfigured: targetType === 'webdav' ? true : undefined }
      setTargets(current => editingTarget ? current.map(item => item.id === editingTarget ? { ...item, ...redactedTarget } : item) : [...current, redactedTarget])
      if (!restoreTargetID) setRestoreTargetID(target.id)
      resetTargetForm()
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份目标保存失败，请检查设置。') }
    finally { setSavingTarget(false) }
  }
  async function deleteTarget(target: BackupTarget) {
    setError('')
    try { await request(`/backup/targets/${encodeURIComponent(target.id)}`, { method: 'DELETE' }); setTargets(current => current.filter(item => item.id !== target.id)); setSelectedTargetIDs(current => current.filter(id => id !== target.id)); setTasks(current => current.map(task => ({ ...task, targetIds: task.targetIds.filter(id => id !== target.id) }))); if (restoreTargetID === target.id) setRestoreTargetID('') }
    catch (cause) { setError(cause instanceof Error ? cause.message : '备份目标删除失败，请重试。') }
  }
  async function runBackup() {
    if (runAvailability.disabled || !password.trim() || password !== passwordConfirm) return
    setBackupState('running'); setBackupResult(null); setError('')
    try {
      const task: BackupTask = activeTask ?? { id: newID('task'), name: '手动备份', targetIds: selectedTargetIDs, includeDownloads, retention: 10, enabled: true }
      if (!activeTask) { await request('/backup/tasks', { method: 'POST', body: JSON.stringify(task) }); setTasks(current => [...current, task]); setSelectedTaskID(task.id) }
      else if (activeTask.includeDownloads !== includeDownloads || activeTask.targetIds.join('|') !== selectedTargetIDs.join('|')) {
        const updated = { ...activeTask, targetIds: selectedTargetIDs, includeDownloads }
        await request('/backup/tasks', { method: 'POST', body: JSON.stringify(updated) }); setTasks(current => current.map(item => item.id === updated.id ? updated : item))
      }
      const result = await request<BackupSnapshot>(`/backup/tasks/${encodeURIComponent(task.id)}/run`, { method: 'POST', body: JSON.stringify({ password }) })
      setBackupResult(result); setSnapshots(current => [result, ...current.filter(snapshot => snapshot.id !== result.id)]); setBackupState('success')
    } catch (cause) { setBackupState('error'); setError(cause instanceof Error ? cause.message : '备份失败，请检查目标和密码后重试。') }
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
    <section className="settings-section" aria-labelledby="backup-targets-title">
      <div className="settings-heading"><div><h2 id="backup-targets-title">备份目标</h2><p className="field-hint">一个备份任务可以同时写入多个目标。</p></div><button type="button" className="icon-button" aria-label="刷新备份状态" title="刷新备份状态" onClick={() => void reload()} disabled={loading}><RefreshCw size={16} className={loading ? 'spin' : undefined}/></button></div>
      {targets.length === 0 && <div className="backup-empty"><HardDrive size={20}/><div><strong>{backupEmptyState('no-target').title}</strong><p>{backupEmptyState('no-target').message}</p><button type="button" className="text-button" onClick={() => document.getElementById('backup-target-name')?.focus()}>添加备份目标</button></div></div>}
      <div className="backup-target-list">{targets.map(target => <div className="backup-target-row" key={target.id}><span className="backup-target-icon">{target.type === 'local' ? <HardDrive size={17}/> : <Cloud size={17}/>}</span><div><strong>{target.name}</strong><p>{targetLabels[target.type as BackupTargetType] ?? target.type} · {target.type === 'webdav' ? `${target.endpoint}${target.remotePath ? ` / ${target.remotePath}` : ''}` : target.path || '未填写位置'}</p>{target.type === 'webdav' && <p className="field-hint">{target.credentialConfigured ? '凭据已配置' : '尚未配置凭据'}</p>}</div><div className="backup-row-actions"><button type="button" className="text-button" onClick={() => editTarget(target)}>编辑</button><button type="button" className="icon-button" aria-label={`删除${target.name}`} title="删除目标" onClick={() => void deleteTarget(target)}><Trash2 size={15}/></button></div></div>)}</div>
      <div className="backup-target-editor"><h3>{editingTarget ? '编辑备份目标' : '添加备份目标'}</h3><div className="backup-form-grid"><label>目标类型<select value={targetType} onChange={event => { setTargetType(event.target.value as BackupTargetType); setFolderBrowserOpen(false) }}><option value="local">本地目录</option><option value="webdav">WebDAV</option><option value="s3" disabled>S3 兼容（即将支持）</option></select></label><label>名称<input id="backup-target-name" value={targetName} maxLength={120} placeholder="例如：家庭 NAS" onChange={event => setTargetName(event.target.value)}/></label>{targetType === 'local' ? <div className="backup-form-wide"><PathInput label="目录" ariaLabel="备份目录" value={targetPath} kind="backup-directory" placeholder="填写 Tangentry 服务端上的目录" onChange={setTargetPath}/></div> : targetType === 'webdav' ? <><label className="backup-form-wide">WebDAV 地址<input value={targetEndpoint} placeholder="https://nas.example/dav" onChange={event => { setTargetEndpoint(event.target.value); setTargetTest('idle'); setFolderBrowserOpen(false) }}/></label><label>用户名<input value={targetUsername} autoComplete="username" onChange={event => { setTargetUsername(event.target.value); setTargetTest('idle'); setFolderBrowserOpen(false) }}/></label><label>密码<input type="password" value={targetPassword} autoComplete="new-password" placeholder={editingTarget ? '留空以保留当前密码' : '输入 WebDAV 密码'} onChange={event => { setTargetPassword(event.target.value); setFolderBrowserOpen(false) }}/></label><div className="backup-form-wide backup-remote-path"><label htmlFor="backup-remote-path">远端目录</label><div><input id="backup-remote-path" value={targetRemotePath || '根目录'} readOnly aria-readonly="true"/><button type="button" className="button button-secondary" disabled={!targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured} onClick={() => setFolderBrowserOpen(value => !value)}><FolderOpen size={16}/>浏览…</button></div></div><label className="backup-checkbox backup-form-wide"><input type="checkbox" checked={allowInsecureTLS} onChange={event => { setAllowInsecureTLS(event.target.checked); setFolderBrowserOpen(false) }}/>允许自签名证书</label><div className="backup-editor-actions backup-form-wide"><button type="button" className="button button-secondary" disabled={targetTest === 'testing' || !targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured} onClick={() => void testTarget()}>测试连接</button>{targetTest === 'success' && <span className="backup-success">连接成功</span>}{targetTest === 'error' && <span className="subtitle-error">连接失败</span>}</div>{folderBrowserOpen && <div className="backup-form-wide"><BackupRemoteFolderBrowser target={{ id: editingTarget ?? '', endpoint: targetEndpoint.trim(), username: targetUsername.trim(), password: targetPassword, allowInsecureTLS }} onSelect={path => { setTargetRemotePath(path); setFolderBrowserOpen(false) }} onClose={() => setFolderBrowserOpen(false)}/></div>}</> : <p className="field-hint backup-form-wide">S3 兼容目标即将支持。</p>}</div><div className="backup-editor-actions"><button type="button" className="button button-secondary" disabled={savingTarget || targetType === 's3' || !targetName.trim() || targetType === 'local' && !targetPath.trim() || targetType === 'webdav' && (!targetEndpoint.trim() || !targetUsername.trim() || !targetPassword && !targets.find(item => item.id === editingTarget)?.credentialConfigured)} onClick={() => void saveTarget()}>{savingTarget ? '正在保存…' : editingTarget ? '保存目标' : '添加目标'}</button>{editingTarget && <button type="button" className="text-button" onClick={resetTargetForm}>取消编辑</button>}</div></div>
    </section>

    <section className="settings-section" aria-labelledby="backup-run-title">
      <div className="settings-heading"><div><h2 id="backup-run-title">备份密码与执行</h2><p className="field-hint">密码只在当前页面内存中使用，不会保存到浏览器。</p></div></div>
      {!password && <div className="backup-empty"><KeyRound size={20}/><div><strong>{backupEmptyState('no-password').title}</strong><p>{backupEmptyState('no-password').message}</p><button type="button" className="text-button" onClick={() => document.getElementById('backup-password')?.focus()}>设置备份密码</button></div></div>}<div className="backup-password-grid"><label><span><KeyRound size={14}/>备份密码</span><input id="backup-password" type="password" autoComplete="new-password" value={password} onChange={event => setPassword(event.target.value)} placeholder="设置或修改备份密码"/></label><label><span>确认密码</span><input type="password" autoComplete="new-password" value={passwordConfirm} onChange={event => setPasswordConfirm(event.target.value)} placeholder="再次输入密码"/></label></div>
      <p className="field-hint backup-warning"><CircleAlert size={14}/>忘记密码无法恢复。请将密码保存在安全的密码管理器中。</p>{passwordError && <p className="subtitle-error" role="alert">{passwordError}</p>}
      <label>选择备份任务<select value={selectedTaskID} onChange={event => { const id = event.target.value; const task = tasks.find(item => item.id === id); setSelectedTaskID(id); setSelectedTargetIDs(task?.targetIds ?? []); setIncludeDownloads(Boolean(task?.includeDownloads)) }}><option value="">新建手动备份任务</option>{tasks.map(task => <option key={task.id} value={task.id}>{task.name}</option>)}</select></label><fieldset className="backup-target-checkboxes"><legend>绑定备份目标</legend>{targets.length === 0 ? <span>请先添加目标。</span> : targets.map(target => <label key={target.id}><input type="checkbox" checked={selectedTargetIDs.includes(target.id)} onChange={event => setSelectedTargetIDs(current => event.target.checked ? [...current, target.id] : current.filter(id => id !== target.id))}/>{target.name}</label>)}</fieldset>
      <label className="backup-checkbox"><input type="checkbox" checked={includeDownloads} onChange={event => setIncludeDownloads(event.target.checked)}/>包含下载媒体（默认关闭）</label><p className="field-hint">{BACKUP_SCOPE_NOTICE}</p>
      <div className="backup-run-actions"><button type="button" className="button button-primary" disabled={runAvailability.disabled || Boolean(passwordError) || !password.trim() || !passwordConfirm.trim()} onClick={() => void runBackup()}><Play size={15}/>{backupState === 'running' ? '正在备份…' : '立即备份'}</button>{selectedTargetIDs.length === 0 && <span className="field-hint" role="status">请至少选择一个备份目标</span>}{runAvailability.disabled && selectedTargetIDs.length > 0 && <span className="field-hint" role="status">{runAvailability.reason}</span>}{backupState === 'success' && backupResult && <span className={`backup-status backup-status-${backupStatusPresentation(backupResult.status).tone}`} role="status"><Check size={14}/>{backupStatusPresentation(backupResult.status).label}：{formatBackupDate(backupResult.createdAt)}，{formatBackupSize(backupResult.size)}</span>}</div>
    </section>

    <section className="settings-section" aria-labelledby="backup-snapshots-title"><div className="settings-heading"><div><h2 id="backup-snapshots-title">快照历史</h2><p className="field-hint">每个快照都可以单独校验完整性。</p></div></div>{snapshots.length === 0 ? <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p><button type="button" className="text-button" onClick={() => document.getElementById('backup-password')?.focus()}>立即备份</button></div></div> : <div className="backup-snapshot-list">{snapshots.map(snapshot => { const status = backupStatusPresentation(snapshot.status); const hasValidation = Object.prototype.hasOwnProperty.call(validationBySnapshot, snapshot.id); const validation = validationBySnapshot[snapshot.id]; const details = validation ? backupValidationPresentation(validation) : null; const retryDisabled = !snapshot.retryAvailable; return <article className="backup-snapshot-row" key={snapshot.id}><div className="backup-snapshot-main"><strong>{formatBackupDate(snapshot.createdAt)}</strong><small>{snapshot.id}</small><span className={`backup-status backup-status-${status.tone}`}>{status.label}</span>{snapshot.targets?.map(target => <span key={target.targetId} className={`backup-status backup-status-${target.status === 'succeeded' ? 'success' : target.status === 'failed' ? 'danger' : 'neutral'}`}>{target.name || target.targetId}：{target.status === 'succeeded' ? '成功' : target.status === 'failed' ? `失败${target.error ? `：${target.error}` : ''}` : target.status}</span>)}{retryDisabled && snapshot.retryUnavailableReason && <span className="field-hint" role="status">{snapshot.retryUnavailableReason}</span>}{hasValidation && <span className={`backup-status backup-status-${validation ? 'success' : 'danger'}`}>{validation ? '校验通过' : '校验失败'}</span>}{details && <details className="backup-validation-details"><summary>查看全部文件校验</summary>{details.files.map(file => <div key={file.path}><code>{file.path}</code><span>{file.size} · SHA-256 {file.sha256}</span></div>)}</details>}</div><div className="backup-snapshot-actions"><span>{formatBackupSize(snapshot.size)}</span>{snapshot.status !== 'succeeded' && <button type="button" className="button button-secondary" disabled={retryingID !== null || retryDisabled} title={retryDisabled ? snapshot.retryUnavailableReason : '仅重试失败目标'} onClick={() => void retryFailed(snapshot.id)}><RefreshCw size={14} className={retryingID === snapshot.id ? 'spin' : undefined}/>{retryingID === snapshot.id ? '重试中…' : '仅重试失败目标'}</button>}<button type="button" className="button button-secondary" disabled={validatingID === snapshot.id || !password.trim() || !selectedTarget} onClick={() => void validateSnapshot(snapshot.id)}>{validatingID === snapshot.id ? '校验中…' : hasValidation ? '重新校验' : '校验'}</button></div></article> })}</div>}</section>

    <section className="settings-section" aria-labelledby="backup-restore-title"><div className="settings-heading"><div><h2 id="backup-restore-title">恢复</h2><p className="field-hint">恢复前会预检快照，确认后需要重启应用才会生效。</p></div></div><div className="backup-stepper">{(['select-snapshot', 'enter-password', 'preflight', 'confirm'] as const).map((step, index) => { const presentation = restoreStepPresentation(step); return <div className={restore.step === step ? 'active' : ''} key={step}><span>{presentation.number}</span><small>{presentation.title}</small>{index < 3 && <ChevronRight size={14}/>}</div> })}</div>
      <div className="backup-restore-form"><label>恢复目标<select value={restoreTargetID} onChange={event => setRestoreTargetID(event.target.value)} disabled={!targets.length}><option value="">请选择目标</option>{targets.map(target => <option key={target.id} value={target.id}>{target.name}</option>)}</select></label><label>选择快照<select value={restore.selectedSnapshotId} onChange={event => chooseRestoreSnapshot(event.target.value)} disabled={!snapshots.length}><option value="">请选择快照</option>{snapshots.map(snapshot => <option key={snapshot.id} value={snapshot.id}>{formatBackupDate(snapshot.createdAt)} · {snapshot.id}</option>)}</select></label>{restore.step === 'enter-password' && <div className="backup-restore-step"><label>备份密码<input type="password" autoComplete="off" value={restorePassword} onChange={event => setRestorePassword(event.target.value)} placeholder="输入创建快照时设置的密码"/></label><button type="button" className="button button-secondary" disabled={!restorePassword.trim()} onClick={submitRestorePassword}>继续预检</button></div>}{restore.step === 'preflight' && <div className="backup-restore-step"><p>{restore.error || '确认密码后开始预检。'}</p><button type="button" className="button button-secondary" disabled={!restoreFlowCanValidate(restore) || restoreBusy} onClick={() => void preflightRestore()}>{restore.status === 'validating' ? '校验中…' : '开始预检'}</button></div>}{restoreValidation && <div className="backup-validation-panel"><strong>预检结果</strong><p>格式版本：v{restoreValidation.formatVersion ?? '?'}，数据库架构版本：v{restoreValidation.schemaVersion ?? '?'}，文件数量：{restoreValidation.fileCount}，大小：{restoreValidation.size}</p><p className="field-hint">{restoreValidation.available ? '可恢复性：通过' : '可恢复性：未知'}</p></div>}{restore.step === 'confirm' && <div className="backup-restore-step"><p>确认恢复后会写入恢复计划，重启后应用。运行中的数据不会热切换。</p><button type="button" className="button button-primary" disabled={!restoreFlowCanConfirm(restore) || restoreBusy} onClick={() => void confirmRestore()}>{restoreBusy ? '正在确认…' : '确认恢复'}</button></div>}{restoreFlowNeedsRestart(restore) && <div className="backup-pending-restore" role="status"><Check size={16}/><div><strong>恢复计划待应用</strong><p>需要重启后生效。计划标识：{restore.planId}</p></div><button type="button" className="button button-secondary" disabled={restoreBusy} onClick={() => void cancelRestore()}>取消待应用恢复</button></div>}{restore.error && restore.step !== 'preflight' && <p className="subtitle-error" role="alert">{restore.error}</p>}{restoreNotice && <p className="field-hint" role="status">{restoreNotice}</p>}{!snapshots.length && <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p></div></div>}</div>
    </section>
    {error && <p className="subtitle-error" role="alert">{error}</p>}
  </div>
}
