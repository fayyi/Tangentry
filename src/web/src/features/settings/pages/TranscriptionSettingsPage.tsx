import { TranscriptionSettings } from '../TranscriptionSettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function TranscriptionSettingsPage({ revision }: { revision: number }) {
  return <SettingsActionPage className="settings-action-transcription" eyebrow="连接" title="字幕转录" description="选择本机或云端识别方式，再配置当前需要的引擎。">
    <TranscriptionSettings key={revision} />
  </SettingsActionPage>
}
