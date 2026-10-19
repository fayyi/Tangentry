import type { BackupValidation } from '../../shared/api/types'

export const RESTORE_STEP_ORDER = ['select-snapshot', 'enter-password', 'preflight', 'confirm'] as const
export type RestoreStep = typeof RESTORE_STEP_ORDER[number]
export type RestoreFlowStatus = 'idle' | 'ready' | 'validating' | 'validated' | 'confirming' | 'pending-restart' | 'cancelled' | 'error'

export interface RestoreFlowState {
  step: RestoreStep
  status: RestoreFlowStatus
  selectedSnapshotId: string
  hasPassword: boolean
  validation: BackupValidation | null
  planId: string | null
  error: string | null
}

export type RestoreFlowEvent =
  | { type: 'select-snapshot'; snapshotId: string }
  | { type: 'set-password'; hasPassword: boolean }
  | { type: 'submit-password' }
  | { type: 'start-validation' }
  | { type: 'validation-succeeded'; validation: BackupValidation }
  | { type: 'validation-failed'; message?: string }
  | { type: 'start-confirmation' }
  | { type: 'confirmation-succeeded'; planId: string }
  | { type: 'confirmation-failed'; message?: string }
  | { type: 'cancel-pending' }
  | { type: 'reset' }

export interface RestoreStepPresentation {
  number: number
  title: string
  description: string
}

export function createRestoreFlow(): RestoreFlowState {
  return { step: 'select-snapshot', status: 'idle', selectedSnapshotId: '', hasPassword: false, validation: null, planId: null, error: null }
}

export function restoreFlowTransition(state: RestoreFlowState, event: RestoreFlowEvent): RestoreFlowState {
  if (event.type === 'reset') return createRestoreFlow()
  if (event.type === 'select-snapshot') {
    const snapshotId = event.snapshotId.trim()
    if (!snapshotId) return { ...state, selectedSnapshotId: '', step: 'select-snapshot', status: 'idle', validation: null, planId: null, error: null }
    return { ...state, selectedSnapshotId: snapshotId, step: 'enter-password', status: 'ready', validation: null, planId: null, error: null }
  }
  if (event.type === 'set-password') {
    const hasPassword = event.hasPassword
    return { ...state, hasPassword, step: state.selectedSnapshotId ? 'enter-password' : 'select-snapshot', status: hasPassword ? 'ready' : state.selectedSnapshotId ? 'idle' : state.status, validation: hasPassword ? state.validation : null, planId: null, error: null }
  }
  if (event.type === 'submit-password') {
    if (!state.selectedSnapshotId) return fail(state, '请先选择要恢复的快照。')
    if (!state.hasPassword) return fail(state, '请输入备份密码。忘记密码无法恢复。')
    return { ...state, step: 'preflight', status: 'ready', validation: null, planId: null, error: null }
  }
  if (event.type === 'start-validation') {
    if (state.step !== 'preflight' || !state.selectedSnapshotId || !state.hasPassword) return fail(state, '请先选择快照并输入备份密码。')
    return { ...state, status: 'validating', error: null }
  }
  if (event.type === 'validation-succeeded') {
    if (state.status !== 'validating') return state
    return { ...state, step: 'confirm', status: 'validated', validation: event.validation, error: null }
  }
  if (event.type === 'validation-failed') {
    return { ...state, step: 'preflight', status: 'error', validation: null, error: event.message?.trim() || '快照校验失败，请选择其他快照或检查备份密码。' }
  }
  if (event.type === 'start-confirmation') {
    if (state.step !== 'confirm' || (state.status !== 'validated' && state.status !== 'error') || !state.validation) return fail(state, '请先完成快照预检。')
    return { ...state, status: 'confirming', error: null }
  }
  if (event.type === 'confirmation-succeeded') {
    const planId = event.planId.trim()
    if (!planId || state.status !== 'confirming') return state
    return { ...state, status: 'pending-restart', planId, error: null }
  }
  if (event.type === 'confirmation-failed') {
    return { ...state, status: 'error', error: event.message?.trim() || '恢复确认失败，请重试。' }
  }
  if (event.type === 'cancel-pending') {
    if (state.status !== 'pending-restart' || !state.planId) return state
    return { ...state, status: 'cancelled', planId: null, error: null }
  }
  return state
}

export function restoreStepPresentation(step: RestoreStep): RestoreStepPresentation {
  switch (step) {
    case 'select-snapshot': return { number: 1, title: '选择快照', description: '选择要恢复的备份版本。' }
    case 'enter-password': return { number: 2, title: '输入备份密码', description: '输入创建快照时设置的密码；忘记密码无法恢复。' }
    case 'preflight': return { number: 3, title: '预检快照', description: '确认格式、数据库架构、文件数量、大小和可恢复性。' }
    case 'confirm': return { number: 4, title: '确认恢复', description: '确认后会生成恢复计划，需要重启应用后生效。' }
  }
}

export function restoreFlowCanValidate(state: RestoreFlowState): boolean {
  return state.step === 'preflight' && (state.status === 'ready' || state.status === 'error') && Boolean(state.selectedSnapshotId && state.hasPassword)
}

export function restoreFlowCanConfirm(state: RestoreFlowState): boolean {
  return state.step === 'confirm' && (state.status === 'validated' || state.status === 'error') && state.validation !== null
}

export function restoreFlowNeedsRestart(state: RestoreFlowState): boolean {
  return state.status === 'pending-restart'
}

export function restoreFlowCanCancel(state: RestoreFlowState): boolean {
  return state.status === 'pending-restart' && Boolean(state.planId)
}

function fail(state: RestoreFlowState, message: string): RestoreFlowState {
  return { ...state, status: 'error', error: message }
}
