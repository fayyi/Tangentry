import { useEffect, useRef, useState } from 'react'
import type { ContentItem } from '../api/types'
import { publicationText } from './ContentCard'
import { cachedNote } from '../data/noteDetails'
import { cachedPublication, readPublication } from '../data/publicationDetails'
import { retryThroughProxy, sourceImageURL } from '../media/sourceImage'
import { platformLabel } from './cardHelpers'

export function AuthorAvatar({ item }: { item: ContentItem }) {
  const node = useRef<HTMLDivElement>(null)
  const [resolved, setResolved] = useState(() => cachedPublication(item.id))
  const note = item.platform === 'xiaohongshu' ? cachedNote(item.id) : undefined
  const cachedAvatar = item.authorAvatarUrl || note?.authorAvatarUrl || resolved?.authorAvatarUrl
  useEffect(() => {
    setResolved(cachedPublication(item.id))
    const cachedNoteValue = item.platform === 'xiaohongshu' ? cachedNote(item.id) : undefined
    if (item.authorAvatarUrl || cachedNoteValue?.authorAvatarUrl || (item.platform !== 'youtube' && item.platform !== 'xiaohongshu')) return
    let active = true
    const observer = new IntersectionObserver(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return
      observer.disconnect()
      void readPublication(item, true).then(value => { if (active) setResolved(value) })
    })
    if (node.current) observer.observe(node.current)
    return () => { active = false; observer.disconnect() }
  }, [item.id, item.url, item.authorAvatarUrl, item.platform])
  const avatar = cachedAvatar
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [avatar])
  return <div ref={node} className="author-avatar">{avatar && !failed
    ? <img src={sourceImageURL(item,avatar)} alt="" loading="lazy" referrerPolicy="no-referrer" onError={event => retryThroughProxy(event, item, avatar) || setFailed(true)}/>
    : <div className="author-avatar-placeholder">{item.author ? item.author[0].toUpperCase() : '?'}</div>}</div>
}

export function AuthorName({ item, showPlatform = true }: { item: ContentItem; showPlatform?: boolean }) {
  return <span className="card-source-line"><span className="author-name">{item.author || (item.platform === 'rss' ? '订阅来源' : '未知作者')}</span>{showPlatform && <span className="card-platform" data-platform={item.platform}>{platformLabel(item.platform)}</span>}</span>
}

export function AuthorPublication({ item }: { item: ContentItem }) {
  const node = useRef<HTMLSpanElement>(null)
  const [resolved, setResolved] = useState(() => cachedPublication(item.id))
  const [checked, setChecked] = useState(false)
  const detail = item.platform === 'xiaohongshu' && !item.publishedAt ? cachedNote(item.id) : undefined
  useEffect(() => {
    setResolved(cachedPublication(item.id))
    setChecked(false)
    if (publicationText(item) || (item.platform === 'xiaohongshu' && detail) || !['youtube', 'xiaohongshu'].includes(item.platform)) return
    let disposed = false
    const observer = new IntersectionObserver(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return
      observer.disconnect()
      void readPublication(item).then(value => { if (!disposed) { setResolved(value); setChecked(true) } })
    })
    if (node.current) observer.observe(node.current)
    return () => { disposed = true; observer.disconnect() }
  }, [item.id, item.url, item.publishedAt, item.publishedText, item.platform, detail?.publishedAt])
  const label = publicationText(detail?.publishedAt ? detail : resolved?.publishedAt || resolved?.publishedText ? resolved : item)
  return <span ref={node} className="author-meta">{label || (['youtube', 'xiaohongshu'].includes(item.platform) ? checked ? '发布时间未知' : '日期待获取' : '')}</span>
}
