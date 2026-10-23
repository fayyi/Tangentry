import { Button } from '../../../components/ui'
import type { ContentItem } from '../../api/types'
import { CardTitle } from '../CardText'

export function GalleryCardTitle({ item, onRead }: { item: ContentItem; onRead: (item: ContentItem) => void }) {
  return (
    <Button variant="quiet" className="modern-card-title-btn" data-content-id={item.id} aria-label={`阅读 ${item.title}`} onClick={() => onRead(item)}>
      <CardTitle contentId={item.id} text={item.title} className="modern-card-title" />
    </Button>
  )
}
