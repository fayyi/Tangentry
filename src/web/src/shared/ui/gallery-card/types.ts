import type { ReactNode } from 'react'
import type { ContentItem, ReadingProgress } from '../../api/types'

export interface GalleryCardProps {
  item: ContentItem
  origin?: string
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
