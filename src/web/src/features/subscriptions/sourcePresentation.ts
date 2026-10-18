import type { SubscriptionSource } from '../../shared/api/types'

export const isYouTubeSource = (source: SubscriptionSource) => source.id.startsWith('youtube:channel:')
export const isPaperSource = (source: SubscriptionSource) => source.id.startsWith('arxiv:source:') || source.id.startsWith('huggingface:source:')
export const supportsRefresh = (source: SubscriptionSource) => source.id.startsWith('rss:feed:') || isYouTubeSource(source) || source.id.startsWith('bilibili:creator:') || isPaperSource(source)
export const sourceLabel = (source: SubscriptionSource) => isYouTubeSource(source) ? 'YouTube' : source.id.startsWith('arxiv:') ? 'arXiv' : source.id.startsWith('huggingface:') ? 'HF 每日论文' : source.id.startsWith('rss:') ? 'RSS' : `UID ${source.creatorId}`
export const sourceLinkLabel = (source: SubscriptionSource) => isYouTubeSource(source) ? '打开 YouTube 频道' : source.id.startsWith('arxiv:') ? `arXiv 分类 · ${source.creatorId}` : source.id.startsWith('huggingface:') ? '打开 Hugging Face Papers' : source.id.startsWith('rss:') ? '打开订阅源' : `B站主页 · UID ${source.creatorId}`
