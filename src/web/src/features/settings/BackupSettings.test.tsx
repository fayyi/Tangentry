import { renderToString } from 'react-dom/server'
import { expect, test } from 'vitest'
import { BackupSettings } from './BackupSettings'
import { normalizeBackupSettingsData } from './backupFolderBrowser'

test('backup settings render safely before empty API responses arrive', () => {
  const html = renderToString(<BackupSettings />)
  expect(html).toContain('备份目标')
})

test('backup settings can initialize with empty or missing API data', () => {
  const targets = normalizeBackupSettingsData<{ id: string }>(undefined)
  const tasks = normalizeBackupSettingsData<{ id: string }>(null)
  const snapshots = normalizeBackupSettingsData<{ id: string }>([])
  expect(targets).toEqual([])
  expect(tasks).toEqual([])
  expect(snapshots).toEqual([])
})
