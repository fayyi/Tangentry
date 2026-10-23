import type { useDownloads } from '../../downloads/useDownloads'
import { StorageSettings } from '../StorageSettings'
import { SettingsActionPage } from '../ui/SettingsActionPage'

export function StorageSettingsPage({ downloads, onModelDirectoryChanged }: { downloads: ReturnType<typeof useDownloads>; onModelDirectoryChanged: () => void }) {
  return <SettingsActionPage className="settings-action-storage" eyebrow="本地数据" title="下载与存储" description="选择文件位置，并处理下载工具的安装与更新。">
    <StorageSettings downloads={downloads} onModelDirectoryChanged={onModelDirectoryChanged} />
  </SettingsActionPage>
}
