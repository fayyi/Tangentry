import { Image as ImageIcon } from 'lucide-react'
import { GalleryCardFrame } from './GalleryCardFrame'
import { GalleryCardTitle } from './GalleryCardText'
import { SaveCardAction } from './GalleryCardActions'
import { useCardCover } from './useCardCover'
import type { GalleryCardContentProps } from './types'
import { retryThroughProxy, sourceImageURL } from '../../media/sourceImage'
import { AuthorAvatar, AuthorName, AuthorPublication } from '../CardAuthor'

export function ImageGalleryCard({ item, origin, saved, pending, onRead, onSave, status, excerpt, discovery = false }: GalleryCardContentProps) {
  const { failed: coverFailed, setFailed: setCoverFailed } = useCardCover(item.thumbnailUrl)
  return (
    <GalleryCardFrame item={item} origin={origin} className="card-image" onRead={onRead} status={status} discovery={discovery}>
      <button className="modern-card-thumbnail" onClick={() => onRead(item)} aria-label={`查看图集 ${item.title}`}>
        {item.thumbnailUrl && !coverFailed ? (
          <img src={sourceImageURL(item, item.thumbnailUrl)} alt="" loading="lazy" referrerPolicy="no-referrer" onError={event => retryThroughProxy(event, item, item.thumbnailUrl) || setCoverFailed(true)} />
        ) : (
          <div className="thumbnail-placeholder"><ImageIcon size={48} /></div>
        )}
        {item.images && item.images.length > 1 && <span className="card-image-count"><ImageIcon size={14} />{item.images.length} 张</span>}
      </button>
      <div className="modern-card-content">
        <GalleryCardTitle item={item} onRead={onRead} />
        {excerpt}
        <div className="modern-card-author">
          <AuthorAvatar item={item} />
          <div className="author-info">
            <AuthorName item={item} />
            <AuthorPublication item={item} />
          </div>
          <div className="modern-card-actions"><SaveCardAction item={item} saved={saved} pending={pending} onSave={onSave} /></div>
        </div>
      </div>
    </GalleryCardFrame>
  )
}
