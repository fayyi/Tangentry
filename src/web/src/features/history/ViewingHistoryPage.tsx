import { Button, Input } from '../../components/ui'
import { ArrowRight, Clock3, Search, Trash2 } from 'lucide-react'
import { useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { CardCollection } from '../../shared/ui/CardCollection'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { MediaFilters } from '../../shared/ui/MediaFilters'
import { useProgressOverview } from '../progress/useProgressOverview'
import type { useViewingHistory } from './useViewingHistory'
import { PageHeader } from '../../shared/ui/PageHeader'

function visitDay(openedAt: number) {
  const date = new Date(openedAt)
  const today = new Date()
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
  const key = `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`
  const label = date.toDateString() === today.toDateString() ? '今天'
    : date.toDateString() === yesterday.toDateString() ? '昨天'
    : date.toLocaleDateString('zh-CN', { ...(date.getFullYear() !== today.getFullYear() ? { year: 'numeric' as const } : {}), month: 'long', day: 'numeric' })
  return { key, label }
}

export function ViewingHistoryPage({ history, onRead, onSave, savedIDs, pending, unavailable }: {
  history: ReturnType<typeof useViewingHistory>; onRead: (item: ContentItem) => void; onSave: (item: ContentItem) => void
  savedIDs: Set<string>; pending: Set<string>; unavailable: boolean
}) {
  const [query, setQuery] = useState('')
  const [confirmClear, setConfirmClear] = useState(false)
  const [kind, setKind] = useState('all')
  const overview = useProgressOverview(history.entries.filter(entry => entry.item.kind === 'video' || entry.item.kind === 'audio').map(entry => entry.item.id), true)
  const entries = history.entries.filter(({ item }) => (kind === 'all' || item.kind === kind) && `${item.title} ${item.author}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))
  const days = new Map<string, { label: string; entries: typeof entries }>()
  for (const entry of entries) {
    const { key, label } = visitDay(entry.openedAt)
    const day = days.get(key) ?? { label, entries: [] }
    day.entries.push(entry)
    days.set(key, day)
  }
  return <section className="history-workspace" aria-label="观看与阅读历史">
    <PageHeader title="历史" summary={history.entries.length ? `${entries.length} / ${history.entries.length} 条记录` : '打开过的内容会按日期留在这里'}/>
    {confirmClear && <div className="inline-feedback history-confirm" role="alert"><p>清空历史？收藏、笔记和进度不会删除。</p><Button variant="secondary" className="button button-secondary" onClick={() => setConfirmClear(false)}>取消</Button><Button variant="primary" className="button button-primary" onClick={() => { history.clear(); setConfirmClear(false) }}>确认清空</Button></div>}
    {history.error && <p role="alert">{history.error}</p>}
    <div className="history-toolbar"><MediaFilters value={kind} onChange={setKind}/><label className="collection-query"><Search size={16} aria-hidden="true"/><Input type="search" aria-label="搜索历史" placeholder="搜索标题或作者" value={query} onChange={event => setQuery(event.target.value)}/></label>{history.entries.length > 0 && <Button variant="secondary" className="button button-secondary history-clear" aria-label="清空历史" title="清空历史" onClick={() => setConfirmClear(true)}><Trash2 size={15}/><span>清空历史</span></Button>}</div>
    {entries.length ? [...days].map(([key, day]) => <section className="history-day" key={key} aria-label={`${day.label}的历史`}><h2><Clock3 size={15} aria-hidden="true"/>{day.label}<span className="count">{day.entries.length}</span></h2><CardCollection className="mixed-gallery" data-media={kind} masonry={kind === 'all' || kind === 'image' || kind === 'pdf'} aria-label="历史内容">{day.entries.map(({ item, openedAt }) => <ModernGalleryCard key={item.id} item={item} saved={savedIDs.has(item.id)} pending={unavailable || pending.has(item.id)} onRead={onRead} onSave={onSave} progress={overview.get(item.id)} status={<div className="history-card-footer"><time dateTime={new Date(openedAt).toISOString()}>{new Date(openedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })} 访问</time><Button variant="quiet" iconOnly className="icon-button" aria-label={`删除历史 ${item.title}`} title="删除记录" onClick={event => { event.stopPropagation(); history.remove(item.id) }}><Trash2 size={14}/></Button></div>}/>)}</CardCollection></section>) : <section className="empty-state"><div className="empty-art" aria-hidden="true"><Clock3 size={28} strokeWidth={1.3}/></div><h2>{query || kind !== 'all' ? '没有匹配的历史' : '暂无观看与阅读记录'}</h2><p>{query || kind !== 'all' ? '调整媒体类型或关键词，找回读过的内容。' : '打开过的视频、文章和播客都会留在这里。'}</p>{query || kind !== 'all' ? <Button variant="secondary" className="button button-secondary" onClick={() => { setQuery(''); setKind('all') }}>查看全部历史</Button> : <a className="empty-link" href="/">去发现内容<ArrowRight size={15}/></a>}</section>}
  </section>
}
