import { FileText } from 'lucide-react'
import { GalleryCardFrame } from './GalleryCardFrame'
import { GalleryCardTitle } from './GalleryCardText'
import { SaveCardAction } from './GalleryCardActions'
import type { GalleryCardContentProps } from './types'
import { AuthorName } from '../CardAuthor'

export function PdfGalleryCard({ item, origin, discoveryReason, discoveryType, saved, pending, onRead, onSave, status, excerpt, discovery = false, onFeedback }: GalleryCardContentProps) {
  return (
    <GalleryCardFrame item={item} origin={origin} discoveryReason={discoveryReason} className="card-pdf" onRead={onRead} status={status} onFeedback={discovery ? onFeedback : undefined}>
      <button className="modern-card-thumbnail" onClick={() => onRead(item)} aria-label={`打开 PDF ${item.title}`}>
        <div className="thumbnail-placeholder" aria-hidden="true">PDF</div>
      </button>
      <div className="modern-card-content">
        <GalleryCardTitle item={item} onRead={onRead} />
        {excerpt}
        <div className="modern-card-author">
          <div className="author-avatar"><FileText size={20} /></div>
          <div className="author-info"><AuthorName item={item} discoveryType={discovery ? discoveryType : undefined} discoveryReason={discoveryReason} /></div>
          <div className="modern-card-actions"><SaveCardAction item={item} saved={saved} pending={pending} onSave={onSave} /></div>
        </div>
      </div>
    </GalleryCardFrame>
  )
}
