import { expect, test } from 'vitest'
import { createRestoreFlow, restoreFlowCanCancel, restoreFlowCanConfirm, restoreFlowCanValidate, restoreFlowNeedsRestart, restoreFlowTransition, restoreStepPresentation } from './backupFlow'
import type { BackupValidation } from '../../shared/api/types'

const validation: BackupValidation = {
  snapshotId: 'snapshot-fixture', size: 1024,
  manifest: { formatVersion: 1, schemaVersion: 7, snapshotID: 'snapshot-fixture', includeDownloads: false, files: [] },
}

test('restore flow enforces select, password, preflight and confirmation order', () => {
  let state = createRestoreFlow()
  expect(state.step).toBe('select-snapshot')
  expect(restoreFlowCanValidate(state)).toBe(false)
  state = restoreFlowTransition(state, { type: 'select-snapshot', snapshotId: 'snapshot-fixture' })
  expect(state.step).toBe('enter-password')
  state = restoreFlowTransition(state, { type: 'set-password', hasPassword: true })
  expect(state.step).toBe('enter-password')
  expect(restoreFlowCanValidate(state)).toBe(false)
  state = restoreFlowTransition(state, { type: 'submit-password' })
  expect(state.step).toBe('preflight')
  expect(restoreFlowCanValidate(state)).toBe(true)
  state = restoreFlowTransition(state, { type: 'start-validation' })
  expect(state.status).toBe('validating')
  state = restoreFlowTransition(state, { type: 'validation-succeeded', validation })
  expect(state.step).toBe('confirm')
  expect(restoreFlowCanConfirm(state)).toBe(true)
  state = restoreFlowTransition(state, { type: 'start-confirmation' })
  expect(state.status).toBe('confirming')
  state = restoreFlowTransition(state, { type: 'confirmation-succeeded', planId: 'plan-fixture' })
  expect(state.status).toBe('pending-restart')
  expect(state.planId).toBe('plan-fixture')
  expect(restoreFlowNeedsRestart(state)).toBe(true)
  expect(restoreFlowCanCancel(state)).toBe(true)
  state = restoreFlowTransition(state, { type: 'cancel-pending' })
  expect(state.status).toBe('cancelled')
  expect(state.planId).toBeNull()
  expect(restoreFlowCanCancel(state)).toBe(false)
})

test('restore flow keeps old selection on invalid input and exposes useful errors', () => {
  let state = createRestoreFlow()
  state = restoreFlowTransition(state, { type: 'select-snapshot', snapshotId: 'snapshot-fixture' })
  state = restoreFlowTransition(state, { type: 'submit-password' })
  expect(state.status).toBe('error')
  expect(state.error).toContain('备份密码')
  state = restoreFlowTransition(state, { type: 'set-password', hasPassword: true })
  state = restoreFlowTransition(state, { type: 'submit-password' })
  state = restoreFlowTransition(state, { type: 'start-validation' })
  state = restoreFlowTransition(state, { type: 'validation-failed', message: '校验摘要不匹配。' })
  expect(state.step).toBe('preflight')
  expect(state.status).toBe('error')
  expect(state.error).toBe('校验摘要不匹配。')
  state = restoreFlowTransition(state, { type: 'select-snapshot', snapshotId: '' })
  expect(state.selectedSnapshotId).toBe('')
  expect(state.step).toBe('select-snapshot')
})

test('restore flow cannot skip preflight or confirm a stale plan', () => {
  let state = createRestoreFlow()
  state = restoreFlowTransition(state, { type: 'confirmation-succeeded', planId: 'plan-fixture' })
  expect(state.status).toBe('idle')
  state = restoreFlowTransition(state, { type: 'select-snapshot', snapshotId: 'snapshot-fixture' })
  state = restoreFlowTransition(state, { type: 'set-password', hasPassword: true })
  state = restoreFlowTransition(state, { type: 'start-confirmation' })
  expect(state.status).toBe('error')
  expect(state.error).toContain('快照预检')
})

test('confirmation failure keeps a validated snapshot available for retry', () => {
  let state = createRestoreFlow()
  state = restoreFlowTransition(state, { type: 'select-snapshot', snapshotId: 'snapshot-fixture' })
  state = restoreFlowTransition(state, { type: 'set-password', hasPassword: true })
  state = restoreFlowTransition(state, { type: 'submit-password' })
  state = restoreFlowTransition(state, { type: 'start-validation' })
  state = restoreFlowTransition(state, { type: 'validation-succeeded', validation })
  state = restoreFlowTransition(state, { type: 'start-confirmation' })
  state = restoreFlowTransition(state, { type: 'confirmation-failed', message: '服务暂时不可用。' })
  expect(state.status).toBe('error')
  expect(restoreFlowCanConfirm(state)).toBe(true)
  state = restoreFlowTransition(state, { type: 'start-confirmation' })
  expect(state.status).toBe('confirming')
})

test('restore step copy describes the four-step flow', () => {
  expect(restoreStepPresentation('select-snapshot')).toEqual({ number: 1, title: '选择快照', description: '选择要恢复的备份版本。' })
  expect(restoreStepPresentation('enter-password').description).toContain('忘记密码无法恢复')
  expect(restoreStepPresentation('preflight').description).toContain('文件数量')
  expect(restoreStepPresentation('confirm').description).toContain('重启')
})
