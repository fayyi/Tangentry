import { Button } from '../../components/ui'
import { Bookmark, BookmarkCheck, Download, ExternalLink, MoreHorizontal } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import type { ContentItem } from '../api/types'
import './card-actions.css'

// Native popovers live in the browser's top layer: a masonry card cannot clip
// them, and Escape / outside click / opening another popover dismiss them.
export function CardActions({ item, saved, pending, onSave, onDownload, children, showMore = true }: {
  item: ContentItem; saved: boolean; pending: boolean
  onSave: (item: ContentItem) => void; onDownload?: (item: ContentItem) => void; children?: ReactNode; showMore?: boolean
}) {
  const id = useId(), trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const canDownload = onDownload && (item.kind === 'video' && ['bilibili', 'youtube', 'douyin', 'xiaohongshu'].includes(item.platform) || item.kind === 'audio' && item.mediaUrl)
  useEffect(() => {
    if (!open) return
    const reposition = () => {
      const button = trigger.current, menu = panel.current
      if (!button || !menu) return
      const rect = button.getBoundingClientRect()
      const width = menu.offsetWidth, height = menu.offsetHeight
      menu.style.left = `${Math.max(8, Math.min(innerWidth - width - 8, rect.right - width))}px`
      menu.style.top = `${Math.max(8, Math.min(innerHeight - height - 8, rect.bottom + height + 6 > innerHeight ? rect.top - height - 6 : rect.bottom + 6))}px`
    }
    reposition()
    panel.current?.querySelector<HTMLElement>('button:not(:disabled),a[href]')?.focus({ preventScroll: true })
    window.addEventListener('resize', reposition)
    window.addEventListener('scroll', reposition, true)
    return () => { window.removeEventListener('resize', reposition); window.removeEventListener('scroll', reposition, true) }
  }, [open])
  return <div className="content-actions">
    <Button variant="quiet" className={saved ? 'content-save is-saved' : 'content-save'} disabled={pending} onClick={() => onSave(item)} aria-label={`${saved ? '取消收藏' : '收藏'} ${item.title}`} title={saved ? '取消收藏' : '收藏'}>
      {saved ? <BookmarkCheck size={17} aria-hidden="true" /> : <Bookmark size={17} aria-hidden="true" />}
    </Button>
    {showMore && <Button variant="quiet" ref={trigger} className="content-more" popoverTarget={id} aria-controls={id} aria-expanded={open} aria-label={`更多操作 ${item.title}`} title="更多操作"><MoreHorizontal size={18} aria-hidden="true" /></Button>}
    {showMore && <div id={id} ref={panel} popover="auto" role="group" aria-label={`内容操作 ${item.title}`} className="content-action-popover"
      onToggle={event => setOpen(event.newState === 'open')} onClick={event => {
        const target = event.target as HTMLElement
        if (target.closest('button:not(:disabled),a[href]')) panel.current?.hidePopover()
      }}>
      {children}
      {canDownload && <Button variant="quiet" aria-label={`下载${item.kind === 'audio' ? '音频' : '视频'} ${item.title}`} onClick={() => onDownload?.(item)}><Download size={16} aria-hidden="true" />{item.kind === 'audio' ? '下载音频' : '下载视频'}</Button>}
      <a href={item.url} target="_blank" rel="noopener noreferrer" aria-label={`打开原内容 ${item.title}`}><ExternalLink size={16} aria-hidden="true" />打开原内容</a>
    </div>}
  </div>
}
