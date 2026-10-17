import type { SubscriptionSource } from '../../shared/api/types'

export const isYouTubeSource = (source: SubscriptionSource) => source.id.startsWith('youtube:channel:')
export const supportsRefresh = (source: SubscriptionSource) => source.id.startsWith('rss:feed:') || isYouTubeSource(source)
export const sourceLabel = (source: SubscriptionSource) => isYouTubeSource(source) ? 'YouTube' : source.id.startsWith('rss:') ? 'RSS' : `UID ${source.creatorId}`
export const sourceLinkLabel = (source: SubscriptionSource) => isYouTubeSource(source) ? '打开 YouTube 频道' : source.id.startsWith('rss:') ? '打开订阅源' : `B站主页 · UID ${source.creatorId}`
