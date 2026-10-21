import { expect, test } from 'vitest'
import { backupFolderBreadcrumbs, backupFolderBrowseRequest, backupFolderBrowserTransition, backupFolderParent, createBackupFolderBrowserState, selectedBackupFolderPath } from './backupFolderBrowser'

test('includes the target type required by the browse API', () => {
  expect(backupFolderBrowseRequest({ endpoint: 'https://dav.example.test', username: 'fixture-user' }, '')).toEqual({
    endpoint: 'https://dav.example.test',
    username: 'fixture-user',
    type: 'webdav',
    folderPath: '',
  })
})

test('folder browser handles an empty directory and loading failure', () => {
  let state = createBackupFolderBrowserState()
  state = backupFolderBrowserTransition(state, { type: 'load-started', path: '' })
  expect(state.status).toBe('loading')
  state = backupFolderBrowserTransition(state, { type: 'load-succeeded', folders: [] })
  expect(state.status).toBe('ready')
  expect(state.folders).toEqual([])
  state = backupFolderBrowserTransition(state, { type: 'load-failed', message: '无法读取远端目录。' })
  expect(state.status).toBe('error')
  expect(state.error).toBe('无法读取远端目录。')
})

test('folder browser enters a child folder, navigates to parent and selects its current path', () => {
  let state = createBackupFolderBrowserState()
  state = backupFolderBrowserTransition(state, { type: 'load-started', path: '' })
  state = backupFolderBrowserTransition(state, { type: 'load-succeeded', folders: [{ name: 'Tangentry', path: 'Tangentry' }] })
  state = backupFolderBrowserTransition(state, { type: 'load-started', path: state.folders[0].path })
  expect(state.path).toBe('Tangentry')
  state = backupFolderBrowserTransition(state, { type: 'load-succeeded', folders: [{ name: 'daily', path: 'Tangentry/daily' }] })
  expect(backupFolderBreadcrumbs(state.path)).toEqual([{ name: '根目录', path: '' }, { name: 'Tangentry', path: 'Tangentry' }])
  expect(backupFolderParent('Tangentry/daily')).toBe('Tangentry')
  expect(backupFolderParent(state.path)).toBe('')
  expect(selectedBackupFolderPath(state)).toBe('Tangentry')
  state = backupFolderBrowserTransition(state, { type: 'load-started', path: '' })
  state = backupFolderBrowserTransition(state, { type: 'load-succeeded', folders: [] })
  expect(selectedBackupFolderPath(state)).toBe('')
})

test('folder browser distinguishes folder creation success, existing and failure', () => {
  let state = createBackupFolderBrowserState()
  state = backupFolderBrowserTransition(state, { type: 'create-name-changed', name: 'Tangentry' })
  state = backupFolderBrowserTransition(state, { type: 'create-started' })
  expect(state.createStatus).toBe('creating')
  state = backupFolderBrowserTransition(state, { type: 'create-succeeded', exists: false })
  expect(state.createStatus).toBe('created')
  state = backupFolderBrowserTransition(state, { type: 'create-succeeded', exists: true })
  expect(state.createStatus).toBe('exists')
  state = backupFolderBrowserTransition(state, { type: 'create-failed', message: '无权限。' })
  expect(state.createStatus).toBe('error')
  expect(state.createError).toBe('无权限。')
})
