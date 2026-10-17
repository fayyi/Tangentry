import type { ReactNode } from 'react'
import type { ContentItem, ReadingProgress } from '../../api/types'

export interface GalleryCardProps {
  item: ContentItem
  /** Kept for callers that still provide a compact origin label. */
  origin?: string
  /** The complete backend reason, shown as the discovery badge tooltip. */
  discoveryReason?: string
  discoveryType?: '订阅' | '推荐' | '探索'
  saved: boolean
  pending: boolean
  onRead: (item: ContentItem) => void
  onSave: (item: ContentItem) => void
  onDownload?: (item: ContentItem) => void
  progress?: ReadingProgress | null
  status?: ReactNode
  discovery?: boolean
}

export interface GalleryCardContentProps extends GalleryCardProps {
  excerpt: ReactNode
}
