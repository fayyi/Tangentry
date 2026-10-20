import type { BackupValidation } from '../../shared/api/types'

export type BackupStatusTone = 'neutral' | 'success' | 'warning' | 'danger'

export interface BackupStatusPresentation {
  label: string
  tone: BackupStatusTone
}

export type BackupEmptyStateKind =
  | 'no-target'
  | 'no-snapshot'
  | 'no-password'
  | 'validation-failed'
  | 'pending-restore'

export interface BackupEmptyState {
  title: string
  message: string
  actionLabel: string
  action: string
}

export interface BackupActionAvailability {
  disabled: boolean
  reason?: string
}

export interface BackupActionAvailabilityInput {
  targetCount?: number
  hasTarget?: boolean
  password?: string
  hasPassword?: boolean
  snapshotCount?: number
  hasSnapshot?: boolean
  selectedSnapshotId?: string
  pendingAction?: 'backup' | 'validate' | 'confirm' | null
  validationStatus?: 'idle' | 'validating' | 'passed' | 'failed'
  restoreStatus?: 'idle' | 'confirming' | 'pending-restart' | 'cancelled' | 'error'
}

export interface BackupActionsAvailability {
  run: BackupActionAvailability
  validate: BackupActionAvailability
  confirm: BackupActionAvailability
  cancelPendingRestore: BackupActionAvailability
}

export const BACKUP_SCOPE_NOTICE = '模型、工具、缓存、Cookie、平台/AI/转录密钥和钥匙串内容不包含在备份中。'

export function formatBackupDate(value: string | number | Date | null | undefined): string {
  const date = toDate(value)
  if (!date) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

export function formatBackupSize(bytes: number | null | undefined): string {
  if (!Number.isFinite(bytes) || !bytes || bytes < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / (1024 ** exponent)
  const rounded = exponent === 0 ? Math.round(value).toString() : value >= 10 ? value.toFixed(0) : value.toFixed(1)
  return `${rounded} ${units[exponent]}`
}

export function backupStatusPresentation(status: string | null | undefined): BackupStatusPresentation {
  const normalized = status?.trim().toLowerCase().replace(/[-\s]/g, '_')
  if (normalized === 'succeeded' || normalized === 'success' || normalized === 'completed' || normalized === 'ok' || normalized === 'valid' || normalized === 'verified') {
    return { label: '成功', tone: 'success' }
  }
  if (normalized === 'running' || normalized === 'in_progress' || normalized === 'pending' || normalized === 'validating' || normalized === 'queued') {
    return { label: normalized === 'validating' ? '校验中' : '进行中', tone: 'neutral' }
  }
  if (normalized === 'failed' || normalized === 'failure' || normalized === 'error' || normalized === 'invalid') {
    return { label: normalized === 'invalid' ? '校验失败' : '失败', tone: 'danger' }
  }
  if (normalized === 'partial_success' || normalized === 'partial') return { label: '部分成功', tone: 'warning' }
  if (normalized === 'cancelled' || normalized === 'canceled') return { label: '已取消', tone: 'warning' }
  if (normalized === 'staged' || normalized === 'awaiting_restart' || normalized === 'pending_restart') return { label: '待重启生效', tone: 'warning' }
  if (normalized === 'restored' || normalized === 'applied') return { label: '已恢复', tone: 'success' }
  return { label: status?.trim() ? '状态未知' : '未开始', tone: 'neutral' }
}

export function formatBackupStatus(status: string | null | undefined): string {
  return backupStatusPresentation(status).label
}

export function backupEmptyState(kind: BackupEmptyStateKind): BackupEmptyState {
  switch (kind) {
    case 'no-target':
      return { title: '尚未配置备份目标', message: '先添加本地目录、WebDAV 或 S3 兼容目标，才能创建备份。', actionLabel: '添加备份目标', action: 'add-target' }
    case 'no-snapshot':
      return { title: '暂无备份快照', message: '配置备份目标并完成一次备份后，这里会显示快照历史。', actionLabel: '立即备份', action: 'create-backup' }
    case 'no-password':
      return { title: '尚未设置备份密码', message: '设置或输入备份密码后才能备份和恢复。忘记密码无法恢复。', actionLabel: '设置备份密码', action: 'set-password' }
    case 'validation-failed':
      return { title: '快照校验失败', message: '无法确认快照内容完整，请选择其他快照或检查备份目标。', actionLabel: '重新校验', action: 'validate-snapshot' }
    case 'pending-restore':
      return { title: '恢复计划待应用', message: '恢复计划将在重启应用后生效，当前运行中的数据不会热切换。', actionLabel: '取消待应用恢复', action: 'cancel-pending-restore' }
  }
}

export function backupValidationPresentation(validation: BackupValidation | null | undefined): {
  available: boolean
  snapshotId?: string
  formatVersion?: number
  schemaVersion?: number
  fileCount: number
  size: string
  includeDownloads: boolean
  files: Array<{ path: string; type: string; size: string; sha256: string }>
} {
  if (!validation) return { available: false, fileCount: 0, size: '0 B', includeDownloads: false, files: [] }
  const files = validation.manifest?.files ?? []
  return {
    available: true,
    snapshotId: validation.snapshotId || validation.manifest?.snapshotID,
    formatVersion: validation.manifest?.formatVersion,
    schemaVersion: validation.manifest?.schemaVersion,
    fileCount: files.length,
    size: formatBackupSize(validation.size),
    includeDownloads: Boolean(validation.manifest?.includeDownloads),
    files: files.map(file => ({ path: file.path, type: file.type, size: formatBackupSize(file.size), sha256: file.sha256 })),
  }
}

export function backupActionAvailability(input: BackupActionAvailabilityInput): BackupActionsAvailability {
  const hasTarget = input.hasTarget ?? (input.targetCount ?? 0) > 0
  const hasPassword = input.hasPassword ?? Boolean(input.password?.trim())
  const hasSnapshot = input.hasSnapshot ?? (input.snapshotCount ?? 0) > 0
  const hasSelectedSnapshot = Boolean(input.selectedSnapshotId?.trim()) && hasSnapshot
  const pending = input.pendingAction

  const busy = (action: NonNullable<typeof pending>) => pending === action
  const run: BackupActionAvailability = busy('backup')
    ? { disabled: true, reason: '备份正在进行，请稍候。' }
    : !hasTarget
      ? { disabled: true, reason: '请先添加备份目标。' }
      : !hasPassword
        ? { disabled: true, reason: '请先设置备份密码。忘记密码无法恢复。' }
        : { disabled: false }
  const validate: BackupActionAvailability = busy('validate')
    ? { disabled: true, reason: '快照校验正在进行，请稍候。' }
    : !hasTarget
      ? { disabled: true, reason: '请先添加备份目标。' }
    : !hasSnapshot
      ? { disabled: true, reason: '暂无快照，请先完成一次备份。' }
      : !hasSelectedSnapshot
        ? { disabled: true, reason: '请选择要校验的快照。' }
        : !hasPassword
          ? { disabled: true, reason: '请先输入备份密码。忘记密码无法恢复。' }
          : { disabled: false }
  const confirm: BackupActionAvailability = input.restoreStatus === 'confirming' || busy('confirm')
    ? { disabled: true, reason: '恢复确认正在进行，请稍候。' }
    : input.validationStatus !== 'passed'
      ? { disabled: true, reason: '请先完成快照预检。' }
      : { disabled: false }
  const cancelPendingRestore: BackupActionAvailability = input.restoreStatus === 'pending-restart'
    ? { disabled: false }
    : { disabled: true, reason: '当前没有待应用的恢复计划。' }
  return { run, validate, confirm, cancelPendingRestore }
}

export function immediateBackupAvailability(input: Pick<BackupActionAvailabilityInput, 'targetCount' | 'hasTarget' | 'password' | 'hasPassword' | 'pendingAction'>): BackupActionAvailability {
  return backupActionAvailability(input).run
}

function toDate(value: string | number | Date | null | undefined): Date | null {
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value
  if (typeof value === 'number') {
    const milliseconds = Math.abs(value) < 1_000_000_000_000 ? value * 1000 : value
    const date = new Date(milliseconds)
    return Number.isNaN(date.getTime()) ? null : date
  }
  if (!value?.trim()) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}
