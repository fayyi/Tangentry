import { Bookmark, Download } from 'lucide-react'
import type { ContentItem } from '../../api/types'

interface SaveProps {
  item: ContentItem
  saved: boolean
  pending: boolean
  onSave: (item: ContentItem) => void
}

interface DownloadProps {
  onDownload?: (item: ContentItem) => void
}

export function SaveCardAction({ item, saved, pending, onSave }: SaveProps) {
  return (
    <button
      className={`card-action-button ${saved ? 'active' : ''}`}
      onClick={() => onSave(item)}
      disabled={pending}
      aria-label={`${saved ? '取消收藏' : '收藏'} ${item.title}`}
      title={saved ? '取消收藏' : '收藏'}
    >
      <Bookmark size={18} fill={saved ? 'currentColor' : 'none'} />
    </button>
  )
}

export function VideoCardActions({ item, saved, pending, onSave, onDownload }: SaveProps & DownloadProps) {
  return (
    <div className="modern-card-actions">
      <SaveCardAction item={item} saved={saved} pending={pending} onSave={onSave} />
      {onDownload && ['bilibili', 'youtube', 'douyin', 'xiaohongshu'].includes(item.platform) && (
        <button className="card-action-button" onClick={() => onDownload(item)} aria-label="下载" title="下载">
          <Download size={17} />
        </button>
      )}
    </div>
  )
}

export function AudioCardActions({ item, saved, pending, onSave, onDownload }: SaveProps & DownloadProps) {
  return (
    <div className="modern-card-actions">
      {onDownload && <button className="card-action-button" onClick={() => onDownload(item)} title="下载音频" aria-label={`下载音频 ${item.title}`}><Download size={16} /></button>}
      <button
        className={`card-action-button ${saved ? 'active' : ''}`}
        onClick={() => onSave(item)}
        disabled={pending}
        aria-label={`${saved ? '取消收藏' : '收藏'} ${item.title}`}
      >
        <Bookmark size={16} fill={saved ? 'currentColor' : 'none'} />
      </button>
    </div>
  )
}
