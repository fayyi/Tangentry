import { BackupSettings } from '../BackupSettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function BackupSettingsPage() {
  return <SettingsActionPage className="settings-action-backup" eyebrow="本地数据" title="备份与恢复" description="执行备份、管理目标，或从已有快照恢复。">
    <BackupSettings />
  </SettingsActionPage>
}
