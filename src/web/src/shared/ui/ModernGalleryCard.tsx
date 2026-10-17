import { cardExcerpt } from './cardDescription'
import { CardExcerpt } from './CardText'
import type { GalleryCardProps } from './gallery-card/types'
import { VideoGalleryCard } from './gallery-card/VideoGalleryCard'
import { AudioGalleryCard } from './gallery-card/AudioGalleryCard'
import { ImageGalleryCard } from './gallery-card/ImageGalleryCard'
import { PdfGalleryCard } from './gallery-card/PdfGalleryCard'
import { ArticleGalleryCard } from './gallery-card/ArticleGalleryCard'

export type { GalleryCardProps }

export function ModernGalleryCard(props: GalleryCardProps) {
  const description = cardExcerpt(props.item)
  const excerpt = description ? <CardExcerpt contentId={props.item.id} text={description} /> : null
  const contentProps = { ...props, excerpt }

  switch (props.item.kind) {
    case 'video':
      return <VideoGalleryCard {...contentProps} />
    case 'audio':
      return <AudioGalleryCard {...contentProps} />
    case 'image':
      return <ImageGalleryCard {...contentProps} />
    case 'pdf':
      return <PdfGalleryCard {...contentProps} />
    case 'article':
      return <ArticleGalleryCard {...contentProps} />
  }
}
