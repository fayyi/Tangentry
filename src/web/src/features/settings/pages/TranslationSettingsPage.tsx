import { AISettings } from '../../ai/AISettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function TranslationSettingsPage() {
  return <SettingsActionPage className="settings-action-translation" eyebrow="连接" title="翻译与摘要" description="连接模型服务后即可测试；任务模型与提示词可按需调整。">
    <AISettings />
  </SettingsActionPage>
}
