import { FileText } from 'lucide-react'
import { GalleryCardFrame } from './GalleryCardFrame'
import { GalleryCardTitle } from './GalleryCardText'
import { SaveCardAction } from './GalleryCardActions'
import type { GalleryCardContentProps } from './types'
import { AuthorName } from '../CardAuthor'

export function PdfGalleryCard({ item, origin, saved, pending, onRead, onSave, status, excerpt, discovery = false }: GalleryCardContentProps) {
  return (
    <GalleryCardFrame item={item} origin={origin} className="card-pdf" onRead={onRead} status={status} discovery={discovery}>
      <button className="modern-card-thumbnail" onClick={() => onRead(item)} aria-label={`打开 PDF ${item.title}`}>
        <div className="thumbnail-placeholder" aria-hidden="true">PDF</div>
      </button>
      <div className="modern-card-content">
        <GalleryCardTitle item={item} onRead={onRead} />
        {excerpt}
        <div className="modern-card-author">
          <div className="author-avatar"><FileText size={20} /></div>
          <div className="author-info"><AuthorName item={item} /></div>
          <div className="modern-card-actions"><SaveCardAction item={item} saved={saved} pending={pending} onSave={onSave} /></div>
        </div>
      </div>
    </GalleryCardFrame>
  )
}
