import { PlatformCookies } from '../PlatformCookies'
import { SettingsActionPage } from '../ui/SettingsActionPage'
import { DiscoveryFeedback } from '../DiscoveryFeedback'
import { YouTubeRecommendations } from '../YouTubeRecommendations'
import type { useDownloads } from '../../downloads/useDownloads'
import type { Feed } from '../../../shared/api/types'
import '../accounts-settings.css'

export function AccountsSettingsPage({ downloads, cookieRevision, onChanged, onFeed, onDiscover }: {
  downloads: ReturnType<typeof useDownloads>
  cookieRevision: number
  onChanged: (platform: string) => void
  onFeed: (feed: Feed) => void
  onDiscover: () => void
}) {
  return <SettingsActionPage className="settings-action-accounts" eyebrow="连接" title="账号" description="选择内容来源，管理登录与推荐。">
    <PlatformCookies onChanged={onChanged} />
    <section className="accounts-discovery-tools" aria-labelledby="accounts-discovery-tools-title">
      <header className="accounts-discovery-tools-head">
        <div>
          <span>账号能力</span>
          <h3 id="accounts-discovery-tools-title">推荐与发现</h3>
          <p>使用已连接的平台账号读取推荐，并管理你对发现内容的反馈。</p>
        </div>
      </header>
      <div className="accounts-discovery-tools-grid">
        <YouTubeRecommendations key={cookieRevision} tools={downloads.tools} onFeed={onFeed} onDiscover={onDiscover} />
        <DiscoveryFeedback />
      </div>
    </section>
  </SettingsActionPage>
}
