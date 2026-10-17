import type { KeyboardEvent, MouseEvent, ReactNode } from 'react'
import type { ContentItem } from '../../api/types'

interface Props {
  item: ContentItem
  origin?: string
  className: string
  onRead: (item: ContentItem) => void
  children: ReactNode
  status?: ReactNode
  dataPlatform?: boolean
  discovery?: boolean
}

export function GalleryCardFrame({ item, origin, className, onRead, children, status, dataPlatform = false, discovery = false }: Props) {
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
      title={origin}
    >
      {discovery && origin && <span className="discovery-origin-label">{origin}</span>}
      {children}
      {status}
    </article>
  )
}
