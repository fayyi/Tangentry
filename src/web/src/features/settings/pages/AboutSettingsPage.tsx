import { UpdaterSettings } from '../UpdaterSettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function AboutSettingsPage() {
  return <SettingsActionPage className="settings-action-about" eyebrow="系统" title="关于与更新" description="查看当前运行环境，并执行可用的更新操作。">
    <UpdaterSettings />
  </SettingsActionPage>
}
