import { MoreVertical } from 'lucide-react'
import { useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import type { ContentItem } from '../../api/types'

interface Props {
  item: ContentItem
  origin?: string
  discoveryReason?: string
  className: string
  onRead: (item: ContentItem) => void
  children: ReactNode
  status?: ReactNode
  dataPlatform?: boolean
	onFeedback?: GalleryFeedbackHandler
}

export type GalleryFeedbackHandler = (type: 'more' | 'less' | 'hide' | 'block_creator', target: 'item' | 'source' | 'creator' | 'topic', value: string) => void

export function GalleryCardFrame({ item, origin, discoveryReason, className, onRead, children, status, dataPlatform = false, onFeedback }: Props) {
  const [menuOpen, setMenuOpen] = useState(false)
  const creator = item.authorId || item.author
  const feedback = (type: 'more' | 'less' | 'hide' | 'block_creator', target: 'item' | 'source' | 'creator' | 'topic', value: string) => {
    onFeedback?.(type, target, value)
    setMenuOpen(false)
  }
  const openCard = (event: MouseEvent<HTMLElement>) => {
    if (item.kind === 'video' && !(event.target as Element).closest('.modern-card-content')) return
    if ((event.target as Element).closest('button, a, input, select, textarea, summary, [role="button"]') || window.getSelection()?.toString()) return
    onRead(item)
  }

  const openCardFromKeyboard = (event: KeyboardEvent<HTMLElement>) => {
    if (event.target !== event.currentTarget || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || (event.key !== 'Enter' && event.key !== ' ')) return
    event.preventDefault()
    if (event.repeat) return
    onRead(item)
  }

  return (
    <article
      tabIndex={0}
      onKeyDown={openCardFromKeyboard}
      className={`modern-card ${className}`}
      onClick={openCard}
      data-testid="content-card"
      aria-label={`阅读 ${item.title}`}
      {...(dataPlatform ? { 'data-platform': item.platform } : {})}
      title={discoveryReason || origin}
    >
      {onFeedback && <div className="discovery-feedback-menu" onClick={event => event.stopPropagation()}>
        <button type="button" className="card-action-button" aria-label="调整发现偏好" title="调整发现偏好" aria-expanded={menuOpen} onClick={() => setMenuOpen(value => !value)}><MoreVertical size={17} /></button>
        {menuOpen && <div className="discovery-feedback-popover" role="menu">
          <strong>调整发现偏好</strong>
          <span>想多看看</span>
          <button type="button" role="menuitem" onClick={() => feedback('more', 'source', item.platform)}>同来源</button>
          <button type="button" role="menuitem" onClick={() => feedback('more', 'creator', creator)}>同创作者</button>
          {item.category && <button type="button" role="menuitem" onClick={() => feedback('more', 'topic', item.category!)}>同主题</button>}
          <span>暂时少些（7 天）</span>
          <button type="button" role="menuitem" onClick={() => feedback('less', 'source', item.platform)}>同来源</button>
          <button type="button" role="menuitem" onClick={() => feedback('less', 'creator', creator)}>同创作者</button>
          {item.category && <button type="button" role="menuitem" onClick={() => feedback('less', 'topic', item.category!)}>同主题</button>}
          <button type="button" role="menuitem" onClick={() => feedback('hide', 'item', item.id)}>不感兴趣</button>
          <button type="button" role="menuitem" onClick={() => feedback('block_creator', 'creator', creator)}>屏蔽此创作者</button>
        </div>}
      </div>}
      {children}
      {status}
    </article>
  )
}
