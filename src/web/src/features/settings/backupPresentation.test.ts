import { expect, test } from 'vitest'
import { BACKUP_SCOPE_NOTICE, backupActionAvailability, backupEmptyState, backupStatusPresentation, backupValidationPresentation, formatBackupDate, formatBackupSize, immediateBackupAvailability } from './backupPresentation'
import type { BackupValidation } from '../../shared/api/types'

const validation: BackupValidation = {
  snapshotId: 'snapshot-fixture', size: 1536,
  manifest: { formatVersion: 1, schemaVersion: 7, snapshotID: 'snapshot-fixture', includeDownloads: false, files: [{ path: 'database.sqlite', type: 'file', size: 1024, sha256: 'redacted-sha256' }] },
}

test('formats backup sizes and dates without locale-sensitive raw values', () => {
  expect(formatBackupSize(0)).toBe('0 B')
  expect(formatBackupSize(1024)).toBe('1.0 KB')
  expect(formatBackupSize(1536)).toBe('1.5 KB')
  expect(formatBackupSize(1024 ** 2 * 2)).toBe('2.0 MB')
  expect(formatBackupSize(-1)).toBe('0 B')
  expect(formatBackupDate('not-a-date')).toBe('时间未知')
  expect(formatBackupDate('2026-01-02T03:04:05Z')).toContain('2026')
})

test('maps backend backup statuses to Chinese labels and tones', () => {
  expect(backupStatusPresentation('succeeded')).toEqual({ label: '成功', tone: 'success' })
  expect(backupStatusPresentation('validating')).toEqual({ label: '校验中', tone: 'neutral' })
  expect(backupStatusPresentation('invalid')).toEqual({ label: '校验失败', tone: 'danger' })
  expect(backupStatusPresentation('fixture-status')).toEqual({ label: '状态未知', tone: 'neutral' })
})

test('empty states provide actionable next steps', () => {
  expect(backupEmptyState('no-target')).toMatchObject({ action: 'add-target', actionLabel: '添加备份目标' })
  expect(backupEmptyState('no-snapshot')).toMatchObject({ action: 'create-backup' })
  expect(backupEmptyState('no-password').message).toContain('忘记密码无法恢复')
  expect(backupEmptyState('validation-failed')).toMatchObject({ action: 'validate-snapshot' })
  expect(backupEmptyState('pending-restore')).toMatchObject({ action: 'cancel-pending-restore' })
})

test('validation presentation exposes manifest versions, file hashes and scope', () => {
  expect(backupValidationPresentation(null)).toEqual({ available: false, fileCount: 0, size: '0 B', includeDownloads: false, files: [] })
  expect(backupValidationPresentation(validation)).toEqual({ available: true, snapshotId: 'snapshot-fixture', formatVersion: 1, schemaVersion: 7, fileCount: 1, size: '1.5 KB', includeDownloads: false, files: [{ path: 'database.sqlite', type: 'file', size: '1.0 KB', sha256: 'redacted-sha256' }] })
  expect(BACKUP_SCOPE_NOTICE).toContain('密钥')
})

test('immediate backup is disabled until a target and password exist and while running', () => {
  expect(immediateBackupAvailability({ targetCount: 0, password: '' })).toMatchObject({ disabled: true, reason: '请先添加备份目标。' })
  expect(immediateBackupAvailability({ targetCount: 1, password: '' })).toMatchObject({ disabled: true })
  expect(immediateBackupAvailability({ targetCount: 1, password: '' }).reason).toContain('请先设置备份密码。')
  expect(immediateBackupAvailability({ targetCount: 1, password: 'fixture-password', pendingAction: 'backup' }).reason).toContain('备份正在进行')
  expect(immediateBackupAvailability({ targetCount: 1, password: 'fixture-password' })).toEqual({ disabled: false })
})

test('restore and validation controls expose their actionable blockers', () => {
  expect(backupActionAvailability({ targetCount: 0, password: 'fixture-password', snapshotCount: 0 }).validate.reason).toContain('备份目标')
  expect(backupActionAvailability({ targetCount: 1, password: 'fixture-password', snapshotCount: 0 }).validate.reason).toContain('暂无快照')
  expect(backupActionAvailability({ targetCount: 1, password: 'fixture-password', snapshotCount: 1 }).validate.reason).toContain('请选择')
  expect(backupActionAvailability({ targetCount: 1, password: 'fixture-password', snapshotCount: 1, selectedSnapshotId: 'snapshot-fixture' }).validate.disabled).toBe(false)
  expect(backupActionAvailability({ validationStatus: 'passed' }).confirm.disabled).toBe(false)
  expect(backupActionAvailability({ restoreStatus: 'pending-restart' }).cancelPendingRestore.disabled).toBe(false)
})
