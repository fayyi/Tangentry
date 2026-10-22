import { Bookmark, Headphones, LoaderCircle, Plus, RefreshCw, Rss, Settings, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { CardCollection } from '../../shared/ui/CardCollection'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { PageHeader } from '../../shared/ui/PageHeader'
import { SkeletonGallery } from '../../shared/ui/SkeletonLoading'
import { CardProgress } from '../progress/ProgressOverview'
import { supportsProgress, useProgressOverview } from '../progress/useProgressOverview'
import type { HistoryEntry } from '../history/useViewingHistory'
import type { useGallery } from '../discovery/useGallery'
import type { usePodcastEpisodes } from './usePodcastEpisodes'
import './podcasts.css'

type Shared = { savedIDs: Set<string>; pending: Set<string>; unavailable: boolean; onRead: (item: ContentItem) => void; onSave: (item: ContentItem) => void; onDownload: (item: ContentItem) => void }

function Section({ id, title, icon, count, action, children }: { id: string; title: string; icon: ReactNode; count?: number; action?: ReactNode; children: ReactNode }) {
  return <section className="podcast-section" aria-labelledby={id}>
    <div className="section-heading"><h2 id={id}>{icon}{title}{count !== undefined && count > 0 && <span className="count">{count}</span>}</h2>{action}</div>
    {children}
  </section>
}

function Episodes({ items, shared, status }: { items: ContentItem[]; shared: Shared; status?: (item: ContentItem) => ReactNode }) {
  return <CardCollection className="mixed-gallery podcast-episodes" data-media="audio" masonry={false} aria-label="播客单集">
    {items.map(item => <ModernGalleryCard key={item.id} item={item} saved={shared.savedIDs.has(item.id)} pending={shared.unavailable || shared.pending.has(item.id)} onRead={shared.onRead} onSave={shared.onSave} onDownload={item.mediaUrl ? shared.onDownload : undefined} status={status?.(item)}/>)}
  </CardCollection>
}

export function PodcastsPage({ history, library, episodes, gallery, onSubscriptions, onSubscribePodcast, onSettings, onAdd, ...shared }: Shared & {
  history: HistoryEntry[]; library: ContentItem[]; episodes: ReturnType<typeof usePodcastEpisodes>; gallery: ReturnType<typeof useGallery>
  onSubscriptions: () => void; onSubscribePodcast: () => void; onSettings: () => void; onAdd: () => void
}) {
  const listened = history.filter(entry => entry.item.kind === 'audio').map(entry => entry.item)
  const progress = useProgressOverview(listened.map(item => item.id), true)
  const resume = listened.filter(item => { const value = progress.get(item.id); return value && !value.completed && value.positionSeconds > 0 }).slice(0, 6)
  const recent = (resume.length ? resume : listened.slice(0, 3))
  const saved = library.filter(item => item.kind === 'audio')
  const discovery = gallery.sources.xiaoyuzhou
  const recommendations = (discovery.feed?.items ?? []).filter(item => item.kind === 'audio').slice(0, 12)
  const recommendationsEnabled = Boolean(gallery.enabled?.xiaoyuzhou)
  const recentListened = listened.slice(0, 6)
  const [section, setSection] = useState<'continue' | 'updates' | 'discover' | 'saved'>('updates')
  return <div className="podcasts-page">
    <PageHeader title="播客" summary={!episodes.loaded ? '正在读取订阅更新…' : episodes.entries.length ? `${episodes.entries.length} 集新更新` : `${resume.length} 集待听 · ${saved.length} 集收藏`} actions={<>
      <button type="button" className="button button-secondary" onClick={onSubscriptions}><Rss size={15}/>管理订阅</button>
      <button type="button" className="button button-primary" onClick={onAdd}><Plus size={15}/>添加播客</button>
    </>}/>
    <nav className="podcast-page-nav" aria-label="播客内容">
      <button type="button" aria-current={section === 'updates' ? 'page' : undefined} onClick={() => setSection('updates')}>订阅更新<span>{episodes.loaded ? episodes.entries.length : '…'}</span></button>
      <button type="button" aria-current={section === 'continue' ? 'page' : undefined} onClick={() => setSection('continue')}>继续收听<span>{resume.length || recentListened.length}</span></button>
      <button type="button" aria-current={section === 'discover' ? 'page' : undefined} onClick={() => setSection('discover')}>小宇宙推荐<span>{recommendations.length || '—'}</span></button>
      <button type="button" aria-current={section === 'saved' ? 'page' : undefined} onClick={() => setSection('saved')}>收藏<span>{saved.length}</span></button>
    </nav>
    {section === 'continue' && <Section id="podcasts-resume" title={resume.length ? '继续收听' : '最近收听'} icon={<Headphones size={16} aria-hidden="true"/>} count={resume.length || recentListened.length}>
      {recent.length ? <Episodes items={recent} shared={shared} status={item => supportsProgress(item.id) ? <CardProgress progress={progress.get(item.id)} supported/> : undefined}/>
        : <div className="podcast-empty"><div><strong>还没有收听记录</strong><p>开始播放任意节目后，未听完的内容会自动留在这里。</p></div><button type="button" className="button button-secondary" onClick={() => setSection('discover')}><Sparkles size={15}/>发现节目</button></div>}
    </Section>}
    {section === 'updates' && <Section id="podcasts-updates" title="订阅节目更新" icon={<Rss size={16} aria-hidden="true"/>} count={episodes.entries.length} action={<button type="button" className="icon-button" aria-label="刷新订阅单集" title="刷新订阅单集" disabled={episodes.loading} onClick={() => void episodes.reload()}>{episodes.loading ? <LoaderCircle size={16} className="spin"/> : <RefreshCw size={16}/>}</button>}>
      {episodes.error && <div className="error-panel" role="alert"><p>{episodes.error}</p><button type="button" className="button button-secondary" onClick={() => void episodes.reload()}>重新读取</button></div>}
      {episodes.entries.length > 0 ? <Episodes items={episodes.entries.map(entry => entry.item)} shared={shared}/>
        : !episodes.loaded ? <SkeletonGallery count={3}/>
        : !episodes.error && <div className="podcast-empty"><div><strong>订阅节目，更新自动汇集在这里</strong><p>添加播客 RSS 后，新单集会直接进入订阅更新。</p></div><button type="button" className="button button-secondary" onClick={onSubscribePodcast}><Plus size={15}/>订阅播客</button></div>}
    </Section>}
    {section === 'discover' && <Section id="podcasts-discover" title="小宇宙推荐" icon={<Sparkles size={16} aria-hidden="true"/>} count={recommendations.length} action={recommendationsEnabled && <button type="button" className="icon-button" aria-label="刷新小宇宙推荐" title="刷新小宇宙推荐" disabled={discovery.loading} onClick={() => gallery.refresh(['xiaoyuzhou'])}>{discovery.loading ? <LoaderCircle size={16} className="spin"/> : <RefreshCw size={16}/>}</button>}>
      {!recommendationsEnabled ? <div className="podcast-empty"><div><strong>连接小宇宙，浏览个性推荐</strong><p>登录并开启推荐后，可直接从这里发现新单集。</p></div><button type="button" className="button button-secondary" onClick={onSettings}><Settings size={15}/>连接小宇宙</button></div>
        : discovery.error && !recommendations.length ? <div className="error-panel" role="alert"><p>{discovery.error}</p><button type="button" className="button button-secondary" disabled={discovery.loading} onClick={() => void gallery.retry('xiaoyuzhou')}>重试</button></div>
        : recommendations.length ? <Episodes items={recommendations} shared={shared}/>
        : discovery.loading || !discovery.known ? <SkeletonGallery count={3}/>
        : <p className="field-hint">小宇宙暂时没有新的推荐。</p>}
    </Section>}
    {section === 'saved' && <Section id="podcasts-saved" title="收藏的单集" icon={<Bookmark size={16} aria-hidden="true"/>} count={saved.length}>
      {saved.length ? <Episodes items={saved} shared={shared}/> : <p className="field-hint">在任意单集上点击收藏，它会留在这里随时继续。</p>}
    </Section>}
  </div>
}
