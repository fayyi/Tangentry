import type { ContentItem } from '../../shared/api/types'

export type DiscoveryFeedbackType = 'more' | 'less' | 'hide' | 'block_creator'
export type DiscoveryFeedbackTarget = 'item' | 'source' | 'creator' | 'topic'

export function feedbackValue(item: ContentItem, target: DiscoveryFeedbackTarget): string {
  if (target === 'item') return item.id
  if (target === 'source') return item.platform
  if (target === 'topic') return item.category?.trim() ?? ''
  return item.authorId || item.author
}

export function feedbackMessage(type: DiscoveryFeedbackType): string {
  if (type === 'more') return '已调整：会多看看这类内容。'
  if (type === 'less') return '已调整：7 天内会少些这类内容。'
  if (type === 'block_creator') return '已屏蔽此创作者，可在设置中解除。'
  return '已隐藏这张卡片。'
}
