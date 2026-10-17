import { ArrowDownToLine, Bookmark, BookmarkCheck, Captions, ExternalLink, X, Film, UserRound, CalendarDays } from 'lucide-react'
import { useEffect, useRef } from 'react'
import type { ContentItem } from '../api/types'
import { Cover, formatDate, formatDuration, mediaLabel } from './ContentCard'

export function PreviewDialog({ item, saved, pending, errorMessage, onSave, onClose, downloading, downloadError, onDownload, onRead }: {
  item: ContentItem
  saved: boolean
  pending: boolean
  errorMessage?: string
  onSave: (item: ContentItem) => void
  onClose: () => void
  downloading: boolean
  downloadError?: string
  onDownload: (item: ContentItem) => void
  onRead: (item: ContentItem) => void
}) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = ref.current
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!dialog?.open) dialog?.showModal()
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { dialog?.close(); document.body.style.overflow = previousOverflow; if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true }) }
  }, [])

  return <dialog ref={ref} className="preview-dialog" aria-labelledby="preview-title" onClose={onClose} onClick={event => {
    if (event.target !== event.currentTarget) return
    const bounds = event.currentTarget.getBoundingClientRect()
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) onClose()
  }}>
    <div className="preview-close-bar"><button className="preview-close icon-button" onClick={onClose} aria-label="关闭预览"><X size={20} /></button></div>
    <Cover item={item} large />
    <div className="preview-body">
      <span className="media-label">{mediaLabel(item)}</span>
      <h2 id="preview-title">{item.title}</h2>
      <div className="preview-meta">
        <span><UserRound size={14} />{item.author || '未知作者'}</span>
        <span><CalendarDays size={14} />{formatDate(item.publishedAt)}</span>
        {item.durationSeconds > 0 && <span><Film size={14} />{formatDuration(item.durationSeconds)}</span>}
      </div>
      <p className="preview-description">{item.description || '来源还没有提供简介。'}</p>
      <div className="preview-actions">
        <a className="button button-primary" href={item.url} target="_blank" rel="noopener noreferrer">{item.kind === 'video' ? '打开原视频' : '打开原内容'} <ExternalLink size={16} /></a>
        <button className="button button-secondary" onClick={() => onSave(item)} disabled={pending}>
          {saved ? <BookmarkCheck size={16} /> : <Bookmark size={16} />}{saved ? '已收藏' : '收藏内容'}
        </button>
        {item.kind === "video" && (item.platform === "bilibili" || item.platform === "youtube") && <button className="button button-secondary" disabled={downloading} onClick={() => onDownload(item)}><ArrowDownToLine size={16} />{downloading ? '已在下载队列' : '下载视频'}</button>}
        <button className="button button-secondary" onClick={() => onRead(item)}><Captions size={16} />{item.platform === 'bilibili' ? '阅读字幕' : '打开阅读器'}</button>
      </div>
      <p className="preview-footnote">收藏保存内容记录，可随时回来阅读。</p>
      {errorMessage && <p className="preview-save-error" role="alert">{errorMessage}</p>}
      {downloadError && <p className="preview-save-error" role="alert">{downloadError}</p>}
    </div>
  </dialog>
}
