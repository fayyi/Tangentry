import { Bookmark, ExternalLink, FileText } from 'lucide-react'
import type { ReactNode } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { CardCollection } from '../../shared/ui/CardCollection'
import './paper.css'

export function PaperCard({ item, saved, pending, onRead, onSave, status, actions }: {
  item: ContentItem
  saved: boolean
  pending: boolean
  onRead: (item: ContentItem) => void
  onSave: (item: ContentItem) => void
  status?: ReactNode
  actions?: ReactNode
}) {
  const paper = item.paper
  return <article className="paper-card">
    <button className="paper-card-open" type="button" onClick={() => onRead(item)} aria-label={`阅读论文 ${item.title}`}>
      <span className="paper-card-type"><FileText size={15} aria-hidden="true"/>论文 · arXiv:{paper?.arxivId ?? item.platformId}</span>
      <strong>{item.title}</strong>
      <span className="paper-card-authors">{paper?.authors.join('、') || item.author || '作者信息暂缺'}</span>
      {paper?.categories.length ? <span className="paper-card-categories">{paper.categories.join(' · ')}</span> : null}
      <span className="paper-card-abstract">{paper?.abstract || item.description}</span>
      {paper?.aiKeywords?.length ? <span className="paper-card-keywords">{paper.aiKeywords.slice(0, 5).map(value => <span key={value}>{value}</span>)}</span> : null}
    </button>
    <div className="paper-card-footer">
      {status}
      <span>{paper?.upvotes !== undefined ? `👍 ${paper.upvotes}` : ''}{paper?.githubStars !== undefined ? ` · GitHub ★ ${paper.githubStars}` : ''}</span>
      {paper?.arxivUrl && <a href={paper.arxivUrl} target="_blank" rel="noopener noreferrer" aria-label="打开 arXiv 论文" title="打开 arXiv 论文"><ExternalLink size={15} aria-hidden="true"/></a>}
      {actions}
      <button type="button" className={`paper-card-save${saved ? ' active' : ''}`} disabled={pending} aria-label={`${saved ? '取消收藏' : '收藏'} ${item.title}`} title={saved ? '取消收藏' : '收藏'} onClick={() => onSave(item)}><Bookmark size={16} fill={saved ? 'currentColor' : 'none'} aria-hidden="true"/></button>
    </div>
  </article>
}

export function PaperCollection({ items, savedIDs, pendingIDs, onRead, onSave }: {
  items: ContentItem[]
  savedIDs: Set<string>
  pendingIDs: Set<string>
  onRead: (item: ContentItem) => void
  onSave: (item: ContentItem) => void
}) {
  return <CardCollection className="paper-collection" aria-label="论文推荐" masonry={false}>
    {items.map(item => <PaperCard key={item.id} item={item} saved={savedIDs.has(item.id)} pending={pendingIDs.has(item.id)} onRead={onRead} onSave={onSave}/>)}</CardCollection>
}
