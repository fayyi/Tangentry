import { GalleryCardFrame } from './GalleryCardFrame'
import { GalleryCardTitle } from './GalleryCardText'
import { SaveCardAction } from './GalleryCardActions'
import { useCardCover } from './useCardCover'
import type { GalleryCardContentProps } from './types'
import { retryThroughProxy, sourceImageURL } from '../../media/sourceImage'
import { AuthorAvatar, AuthorName, AuthorPublication } from '../CardAuthor'

export function ArticleGalleryCard({ item, origin, discoveryReason, discoveryType, saved, pending, onRead, onSave, status, excerpt, discovery = false, onFeedback }: GalleryCardContentProps) {
  const { failed: coverFailed, setFailed: setCoverFailed } = useCardCover(item.thumbnailUrl)
  return (
    <GalleryCardFrame item={item} origin={origin} discoveryReason={discoveryReason} className="card-article" onRead={onRead} status={status} onFeedback={discovery ? onFeedback : undefined}>
      {item.thumbnailUrl && !coverFailed && <button className="modern-card-thumbnail" onClick={() => onRead(item)} aria-label={`阅读文章 ${item.title}`}>
        <img src={sourceImageURL(item, item.thumbnailUrl)} alt="" loading="lazy" referrerPolicy="no-referrer" onError={event => retryThroughProxy(event, item, item.thumbnailUrl) || setCoverFailed(true)} />
      </button>}
      <div className="modern-card-content">
        <GalleryCardTitle item={item} onRead={onRead} />
        {excerpt}
        {(item.category || item.replyCount || item.viewCount) && <div className="modern-card-stats" aria-label="话题统计">
          {item.category && <span>{item.category}</span>}
          {item.replyCount ? <span>{item.replyCount} 回复</span> : null}
          {item.viewCount ? <span>{item.viewCount} 浏览</span> : null}
        </div>}
        <div className="modern-card-author">
          {(item.authorAvatarUrl || item.platform === 'xiaohongshu') && <AuthorAvatar item={item} />}
          <div className="author-info" style={{ flex: 1 }}>
            <AuthorName item={item} discoveryType={discovery ? discoveryType : undefined} discoveryReason={discoveryReason} />
            <AuthorPublication item={item} />
          </div>
          <div className="modern-card-actions"><SaveCardAction item={item} saved={saved} pending={pending} onSave={onSave} /></div>
        </div>
      </div>
    </GalleryCardFrame>
  )
}
