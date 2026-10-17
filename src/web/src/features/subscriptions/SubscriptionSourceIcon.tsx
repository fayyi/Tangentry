import { CirclePlay, Rss, TvMinimal } from 'lucide-react'
import type { SubscriptionSource } from '../../shared/api/types'
import { isYouTubeSource } from './sourcePresentation'

export function SubscriptionSourceIcon({ source }: { source: SubscriptionSource }) {
  const platform = isYouTubeSource(source) ? 'youtube' : source.id.startsWith('rss:') ? 'rss' : 'bilibili'
  const Icon = platform === 'youtube' ? CirclePlay : platform === 'rss' ? Rss : TvMinimal
  return <span className="subscription-source-avatar" data-platform={platform} aria-hidden="true"><Icon size={17} strokeWidth={1.7}/></span>
}
