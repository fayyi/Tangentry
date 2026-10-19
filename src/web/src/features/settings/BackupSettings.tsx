import { Check, ChevronRight, CircleAlert, Cloud, HardDrive, KeyRound, Play, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import type { BackupRestoreResult, BackupSnapshot, BackupTarget, BackupTargetType, BackupTask, BackupValidation } from '../../shared/api/types'
import { PathInput } from './PathInput'
import { BACKUP_SCOPE_NOTICE, backupEmptyState, backupStatusPresentation, backupValidationPresentation, formatBackupDate, formatBackupSize, immediateBackupAvailability } from './backupPresentation'
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
  const [editingTarget, setEditingTarget] = useState<string | null>(null)
  const [savingTarget, setSavingTarget] = useState(false)
  const [password, setPassword] = useState('')
  const [passwordConfirm, setPasswordConfirm] = useState('')
  const [includeDownloads, setIncludeDownloads] = useState(false)
  const [backupState, setBackupState] = useState<'idle' | 'running' | 'success' | 'error'>('idle')
  const [backupResult, setBackupResult] = useState<BackupSnapshot | null>(null)
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
      setTargets(Array.isArray(loadedTargets) ? loadedTargets : [])
      setTasks(Array.isArray(loadedTasks) ? loadedTasks : [])
      setSnapshots(Array.isArray(loadedSnapshots) ? loadedSnapshots : [])
      if (!restoreTargetID && loadedTargets?.[0]) setRestoreTargetID(loadedTargets[0].id)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份设置暂时无法读取，请重试。') }
    finally { setLoading(false) }
  }
  useEffect(() => { void reload() }, [])

  const activeTask = tasks[0]
  const selectedTarget = targets.find(target => target.id === restoreTargetID) ?? targets[0]
  const passwordError = password.length > 0 && password !== passwordConfirm ? '两次输入的密码不一致。' : ''
  const runAvailability = immediateBackupAvailability({ targetCount: targets.length, password, pendingAction: backupState === 'running' ? 'backup' : null })
  const restoreValidation = restore.validation ? backupValidationPresentation(restore.validation) : null

  function resetTargetForm() { setEditingTarget(null); setTargetName(''); setTargetPath(''); setTargetType('local') }
  function editTarget(target: BackupTarget) { setEditingTarget(target.id); setTargetName(target.name); setTargetPath(target.path ?? ''); setTargetType(target.type === 'webdav' || target.type === 's3' ? target.type : 'local') }
  async function saveTarget() {
    if (targetType !== 'local' || !targetName.trim() || !targetPath.trim()) return
    setSavingTarget(true); setError('')
    try {
      const target: BackupTarget = { id: editingTarget ?? newID('target'), name: targetName.trim(), type: targetType, path: targetPath.trim(), enabled: true }
      await request('/backup/targets', { method: 'POST', body: JSON.stringify(target) })
      setTargets(current => editingTarget ? current.map(item => item.id === editingTarget ? target : item) : [...current, target])
      if (!restoreTargetID) setRestoreTargetID(target.id)
      resetTargetForm()
    } catch (cause) { setError(cause instanceof Error ? cause.message : '备份目标保存失败，请检查设置。') }
    finally { setSavingTarget(false) }
  }
  async function deleteTarget(target: BackupTarget) {
    setError('')
    try { await request(`/backup/targets/${encodeURIComponent(target.id)}`, { method: 'DELETE' }); setTargets(current => current.filter(item => item.id !== target.id)); if (restoreTargetID === target.id) setRestoreTargetID('') }
    catch (cause) { setError(cause instanceof Error ? cause.message : '备份目标删除失败，请重试。') }
  }
  async function runBackup() {
    if (runAvailability.disabled || !password.trim() || password !== passwordConfirm) return
    setBackupState('running'); setBackupResult(null); setError('')
    try {
      const task: BackupTask = activeTask ?? { id: newID('task'), name: '手动备份', targetIds: targets.map(target => target.id), includeDownloads, retention: 10, enabled: true }
      if (!activeTask) { await request('/backup/tasks', { method: 'POST', body: JSON.stringify(task) }); setTasks([task]) }
      else if (activeTask.includeDownloads !== includeDownloads || activeTask.targetIds.join('|') !== targets.map(target => target.id).join('|')) {
        const updated = { ...activeTask, targetIds: targets.map(target => target.id), includeDownloads }
        await request('/backup/tasks', { method: 'POST', body: JSON.stringify(updated) }); setTasks([updated])
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
      <div className="backup-target-list">{targets.map(target => <div className="backup-target-row" key={target.id}><span className="backup-target-icon">{target.type === 'local' ? <HardDrive size={17}/> : <Cloud size={17}/>}</span><div><strong>{target.name}</strong><p>{targetLabels[target.type as BackupTargetType] ?? target.type} · {target.path || '未填写位置'}</p>{target.type !== 'local' && <p className="field-hint">此目标类型将按现有服务端契约保存。</p>}</div><div className="backup-row-actions"><button type="button" className="text-button" onClick={() => editTarget(target)}>编辑</button><button type="button" className="icon-button" aria-label={`删除${target.name}`} title="删除目标" onClick={() => void deleteTarget(target)}><Trash2 size={15}/></button></div></div>)}</div>
      <div className="backup-target-editor"><h3>{editingTarget ? '编辑备份目标' : '添加备份目标'}</h3><div className="backup-form-grid"><label>目标类型<select value={targetType} onChange={event => setTargetType(event.target.value as BackupTargetType)}><option value="local">本地目录</option><option value="webdav" disabled>WebDAV（即将支持）</option><option value="s3" disabled>S3 兼容（即将支持）</option></select></label><label>名称<input id="backup-target-name" value={targetName} maxLength={120} placeholder="例如：家庭 NAS" onChange={event => setTargetName(event.target.value)}/></label><div className="backup-form-wide">{targetType === 'local' ? <PathInput label="目录" ariaLabel="备份目录" value={targetPath} kind="backup-directory" placeholder="填写 Tangentry 服务端上的目录" onChange={setTargetPath}/> : <p className="field-hint">当前版本只支持本地目录；WebDAV 和 S3 兼容目标将在后续版本开放。</p>}</div></div><div className="backup-editor-actions"><button type="button" className="button button-secondary" disabled={savingTarget || targetType !== 'local' || !targetName.trim() || !targetPath.trim()} onClick={() => void saveTarget()}>{savingTarget ? '正在保存…' : editingTarget ? '保存目标' : '添加目标'}</button>{editingTarget && <button type="button" className="text-button" onClick={resetTargetForm}>取消编辑</button>}</div></div>
    </section>

    <section className="settings-section" aria-labelledby="backup-run-title">
      <div className="settings-heading"><div><h2 id="backup-run-title">备份密码与执行</h2><p className="field-hint">密码只在当前页面内存中使用，不会保存到浏览器。</p></div></div>
      {!password && <div className="backup-empty"><KeyRound size={20}/><div><strong>{backupEmptyState('no-password').title}</strong><p>{backupEmptyState('no-password').message}</p><button type="button" className="text-button" onClick={() => document.getElementById('backup-password')?.focus()}>设置备份密码</button></div></div>}<div className="backup-password-grid"><label><span><KeyRound size={14}/>备份密码</span><input id="backup-password" type="password" autoComplete="new-password" value={password} onChange={event => setPassword(event.target.value)} placeholder="设置或修改备份密码"/></label><label><span>确认密码</span><input type="password" autoComplete="new-password" value={passwordConfirm} onChange={event => setPasswordConfirm(event.target.value)} placeholder="再次输入密码"/></label></div>
      <p className="field-hint backup-warning"><CircleAlert size={14}/>忘记密码无法恢复。请将密码保存在安全的密码管理器中。</p>{passwordError && <p className="subtitle-error" role="alert">{passwordError}</p>}
      <label className="backup-checkbox"><input type="checkbox" checked={includeDownloads} onChange={event => setIncludeDownloads(event.target.checked)}/>包含下载媒体（默认关闭）</label><p className="field-hint">{BACKUP_SCOPE_NOTICE}</p>
      <div className="backup-run-actions"><button type="button" className="button button-primary" disabled={runAvailability.disabled || Boolean(passwordError) || !password.trim() || !passwordConfirm.trim()} onClick={() => void runBackup()}><Play size={15}/>{backupState === 'running' ? '正在备份…' : '立即备份'}</button>{runAvailability.disabled && <span className="field-hint" role="status">{runAvailability.reason}</span>}{backupState === 'success' && backupResult && <span className="backup-success" role="status"><Check size={14}/>已完成：{formatBackupDate(backupResult.createdAt)}，{formatBackupSize(backupResult.size)}</span>}</div>
    </section>

    <section className="settings-section" aria-labelledby="backup-snapshots-title"><div className="settings-heading"><div><h2 id="backup-snapshots-title">快照历史</h2><p className="field-hint">每个快照都可以单独校验完整性。</p></div></div>{snapshots.length === 0 ? <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p><button type="button" className="text-button" onClick={() => document.getElementById('backup-password')?.focus()}>立即备份</button></div></div> : <div className="backup-snapshot-list">{snapshots.map(snapshot => { const status = backupStatusPresentation(snapshot.status); const hasValidation = Object.prototype.hasOwnProperty.call(validationBySnapshot, snapshot.id); const validation = validationBySnapshot[snapshot.id]; const details = validation ? backupValidationPresentation(validation) : null; return <article className="backup-snapshot-row" key={snapshot.id}><div className="backup-snapshot-main"><strong>{formatBackupDate(snapshot.createdAt)}</strong><small>{snapshot.id}</small><span className={`backup-status backup-status-${status.tone}`}>{status.label}</span>{hasValidation && <span className={`backup-status backup-status-${validation ? 'success' : 'danger'}`}>{validation ? '校验通过' : '校验失败'}</span>}{details && <p className="field-hint">格式 v{details.formatVersion ?? '?'} · 数据库架构 v{details.schemaVersion ?? '?'} · {details.fileCount} 个文件 · {details.size}{details.files[0]?.sha256 ? ` · SHA-256 ${details.files[0].sha256}` : ''}</p>}</div><div className="backup-snapshot-actions"><span>{formatBackupSize(snapshot.size)}</span><button type="button" className="button button-secondary" disabled={validatingID === snapshot.id || !password.trim() || !selectedTarget} onClick={() => void validateSnapshot(snapshot.id)}>{validatingID === snapshot.id ? '校验中…' : hasValidation ? '重新校验' : '校验'}</button></div></article> })}</div>}</section>

    <section className="settings-section" aria-labelledby="backup-restore-title"><div className="settings-heading"><div><h2 id="backup-restore-title">恢复</h2><p className="field-hint">恢复前会预检快照，确认后需要重启应用才会生效。</p></div></div><div className="backup-stepper">{(['select-snapshot', 'enter-password', 'preflight', 'confirm'] as const).map((step, index) => { const presentation = restoreStepPresentation(step); return <div className={restore.step === step ? 'active' : ''} key={step}><span>{presentation.number}</span><small>{presentation.title}</small>{index < 3 && <ChevronRight size={14}/>}</div> })}</div>
      <div className="backup-restore-form"><label>恢复目标<select value={restoreTargetID} onChange={event => setRestoreTargetID(event.target.value)} disabled={!targets.length}><option value="">请选择目标</option>{targets.map(target => <option key={target.id} value={target.id}>{target.name}</option>)}</select></label><label>选择快照<select value={restore.selectedSnapshotId} onChange={event => chooseRestoreSnapshot(event.target.value)} disabled={!snapshots.length}><option value="">请选择快照</option>{snapshots.map(snapshot => <option key={snapshot.id} value={snapshot.id}>{formatBackupDate(snapshot.createdAt)} · {snapshot.id}</option>)}</select></label>{restore.step === 'enter-password' && <div className="backup-restore-step"><label>备份密码<input type="password" autoComplete="off" value={restorePassword} onChange={event => setRestorePassword(event.target.value)} placeholder="输入创建快照时设置的密码"/></label><button type="button" className="button button-secondary" disabled={!restorePassword.trim()} onClick={submitRestorePassword}>继续预检</button></div>}{restore.step === 'preflight' && <div className="backup-restore-step"><p>{restore.error || '确认密码后开始预检。'}</p><button type="button" className="button button-secondary" disabled={!restoreFlowCanValidate(restore) || restoreBusy} onClick={() => void preflightRestore()}>{restore.status === 'validating' ? '校验中…' : '开始预检'}</button></div>}{restoreValidation && <div className="backup-validation-panel"><strong>预检结果</strong><p>格式版本：v{restoreValidation.formatVersion ?? '?'}，数据库架构版本：v{restoreValidation.schemaVersion ?? '?'}，文件数量：{restoreValidation.fileCount}，大小：{restoreValidation.size}</p><p className="field-hint">{restoreValidation.available ? '可恢复性：通过' : '可恢复性：未知'}</p></div>}{restore.step === 'confirm' && <div className="backup-restore-step"><p>确认恢复后会写入恢复计划，重启后应用。运行中的数据不会热切换。</p><button type="button" className="button button-primary" disabled={!restoreFlowCanConfirm(restore) || restoreBusy} onClick={() => void confirmRestore()}>{restoreBusy ? '正在确认…' : '确认恢复'}</button></div>}{restoreFlowNeedsRestart(restore) && <div className="backup-pending-restore" role="status"><Check size={16}/><div><strong>恢复计划待应用</strong><p>需要重启后生效。计划标识：{restore.planId}</p></div><button type="button" className="button button-secondary" disabled={restoreBusy} onClick={() => void cancelRestore()}>取消待应用恢复</button></div>}{restore.error && restore.step !== 'preflight' && <p className="subtitle-error" role="alert">{restore.error}</p>}{restoreNotice && <p className="field-hint" role="status">{restoreNotice}</p>}{!snapshots.length && <div className="backup-empty"><ShieldCheck size={20}/><div><strong>{backupEmptyState('no-snapshot').title}</strong><p>{backupEmptyState('no-snapshot').message}</p></div></div>}</div>
    </section>
    {error && <p className="subtitle-error" role="alert">{error}</p>}
  </div>
}
