import { Button } from '../../components/ui'
import { ImageOff, Play } from 'lucide-react'
import { useState } from 'react'
import type { ReactNode } from 'react'
import type { ContentItem } from '../api/types'
import { retryThroughProxy, sourceImageURL } from '../media/sourceImage'
import { CardActions } from './CardActions'
import { cardExcerpt } from './cardDescription'
import { CardExcerpt, CardTitle } from './CardText'

export function mediaLabel(item: ContentItem) { const platform = ({ bilibili:"B站",youtube:"YouTube",douyin:"抖音",xiaohongshu:"小红书",rss:"RSS",xiaoyuzhou:"小宇宙",pdf:"本机" } as Record<string,string>)[item.platform] ?? item.platform; const kind = { video:"视频",audio:"音频",article:"图文",pdf:"PDF",image:"图文" }[item.kind]; return platform+" · "+kind }

export function formatDuration(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return ''
  const total = Math.floor(seconds)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const rest = String(total % 60).padStart(2, '0')
  return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${rest}` : `${minutes}:${rest}`
}

export function formatDate(timestamp: number) {
  return timestamp ? new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric', year: 'numeric' }).format(new Date(timestamp * 1000)) : '发布时间未知'
}

export function publicationText(item: Pick<ContentItem, 'publishedAt' | 'publishedText'>) {
  return item.publishedAt > 0 ? formatDate(item.publishedAt) : item.publishedText?.trim() || ''
}

export function Cover({ item, large = false, showPlay = true }: { item: ContentItem; large?: boolean; showPlay?: boolean }) {
  const [failed, setFailed] = useState(false)
  return <div className={`content-cover ${large ? 'content-cover-large' : ''}`}>
    {item.thumbnailUrl && !failed
      ? <img src={sourceImageURL(item, item.thumbnailUrl)} alt={item.title} loading={large ? 'eager' : 'lazy'} referrerPolicy="no-referrer" onError={event => retryThroughProxy(event, item, item.thumbnailUrl) || setFailed(true)} />
      : <div className="cover-fallback"><ImageOff size={28} /><span>暂无封面</span></div>}
    {!large && showPlay && (item.kind === 'video' || item.kind === 'audio') && <span className="cover-play"><Play size={18} fill="currentColor" /></span>}
    {item.durationSeconds > 0 && <span className="duration-badge">{formatDuration(item.durationSeconds)}</span>}
  </div>
}

export function ContentCard({ item, saved, pending, onRead, onSave, status, actions, onDownload, headingLevel = 2 }: {
  item: ContentItem
  saved: boolean
  pending: boolean
  onRead: (item: ContentItem) => void
  onSave: (item: ContentItem) => void
  status?: ReactNode
  actions?: ReactNode
  headingLevel?: 2 | 3
  onDownload?: (item:ContentItem)=>void
}) {
  const Heading = headingLevel === 3 ? 'h3' : 'h2'
  const description = cardExcerpt(item)
  return <article className="content-card" data-testid="content-card">
    <Button variant="quiet" className="card-open" data-content-id={item.id} onClick={() => onRead(item)} aria-label={`阅读 ${item.title}`}>
      {item.thumbnailUrl && <Cover item={item} />}
      <div className="card-copy">
        <span className="card-source">{mediaLabel(item)}</span>
        {!item.thumbnailUrl && item.durationSeconds > 0 && <span className="card-runtime">{formatDuration(item.durationSeconds)}</span>}
        <CardTitle contentId={item.id} text={item.title} className="card-title" as={Heading}/>
        {description && <CardExcerpt contentId={item.id} text={description}/>}
        {item.author && <p className="card-author">{item.author}</p>}
        {publicationText(item) && <span className="card-date">{publicationText(item)}</span>}
        {status}
      </div>
    </Button>
    <div className="card-actions"><CardActions item={item} saved={saved} pending={pending} onSave={onSave} onDownload={onDownload}>{actions}</CardActions></div>
  </article>
}
