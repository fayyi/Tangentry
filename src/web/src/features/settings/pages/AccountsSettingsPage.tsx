import { PlatformCookies } from '../PlatformCookies'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function AccountsSettingsPage({ onChanged }: { onChanged: (platform: string) => void }) {
  return <SettingsActionPage className="settings-action-accounts" eyebrow="连接" title="账号" description="选择内容来源，管理登录与推荐。">
    <PlatformCookies onChanged={onChanged} />
  </SettingsActionPage>
}
