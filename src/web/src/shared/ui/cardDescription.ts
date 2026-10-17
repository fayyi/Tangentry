import type { ContentItem } from '../api/types'

export function cardDescription(item: ContentItem) {
  const description = item.description?.trim() ?? ''
  if (item.platform === 'rss' && /^(comments?|discussion|read more|阅读全文|阅读原文|查看评论|评论)$/i.test(description)) return ''
  return description
}

export function cardExcerpt(item: ContentItem) {
  return cardDescription(item) || (item.platform === 'rss' && item.kind === 'article' ? item.previewText?.trim() ?? '' : '')
}
