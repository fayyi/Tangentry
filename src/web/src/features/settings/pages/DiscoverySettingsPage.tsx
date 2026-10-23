import { Check, CirclePlay, MessageSquare } from 'lucide-react'
import { useState } from 'react'
import { Button } from '../../../components/ui'
import { DiscoveryFeedback } from '../DiscoveryFeedback'
import { YouTubeRecommendations } from '../YouTubeRecommendations'
import type { useDownloads } from '../../downloads/useDownloads'
import type { Feed } from '../../../shared/api/types'
import { SettingsActionPage } from '../ui/SettingsActionPage'
import '../accounts-settings.css'

export function DiscoverySettingsPage({ downloads, cookieRevision, onFeed, onDiscover }: {
  downloads: ReturnType<typeof useDownloads>
  cookieRevision: number
  onFeed: (feed: Feed) => void
  onDiscover: () => void
}) {
  const [selected, setSelected] = useState('youtube')
  const options = [
    { id: 'youtube', label: 'YouTube', detail: downloads.tools?.youtubeRecommendation?.available ? '可以检测推荐' : '推荐工具未就绪', Icon: CirclePlay },
    { id: 'feedback', label: '发现反馈', detail: '管理内容偏好', Icon: MessageSquare },
  ]
  return <SettingsActionPage className="settings-action-discovery" eyebrow="体验" title="发现" description="选择推荐来源或反馈，查看对应设置。">
    <div className="accounts-workspace">
      <div className="accounts-platform-layout accounts-discovery-layout">
        <div className="accounts-platform-tiles" role="group" aria-label="选择发现设置">
          {options.map(option => <Button key={option.id} variant="quiet" type="button" className="accounts-platform-tile" aria-pressed={selected === option.id} aria-controls={`discovery-settings-${option.id}`} onClick={() => setSelected(option.id)}>
            <option.Icon size={19} aria-hidden="true"/>
            <span className="accounts-platform-name">{option.label}</span>
            <span className="accounts-platform-status">{option.detail}</span>
            {selected === option.id && <Check className="accounts-platform-selected" size={13} aria-hidden="true"/>}
          </Button>)}
        </div>
        <div className="accounts-platform-panels">
          <section id="discovery-settings-youtube" className="accounts-platform-panel" aria-label="YouTube 推荐来源" hidden={selected !== 'youtube'}>
            <YouTubeRecommendations key={cookieRevision} tools={downloads.tools} onFeed={onFeed} onDiscover={onDiscover}/>
          </section>
          <section id="discovery-settings-feedback" className="accounts-platform-panel" aria-label="发现反馈" hidden={selected !== 'feedback'}>
            <DiscoveryFeedback/>
          </section>
        </div>
      </div>
    </div>
  </SettingsActionPage>
}
