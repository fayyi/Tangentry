import { Bookmark, Headphones, LoaderCircle, Plus, RefreshCw, Rss, Settings, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'
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

export function PodcastsPage({ history, library, episodes, gallery, onSubscriptions, onSettings, onAdd, ...shared }: Shared & {
  history: HistoryEntry[]; library: ContentItem[]; episodes: ReturnType<typeof usePodcastEpisodes>; gallery: ReturnType<typeof useGallery>
  onSubscriptions: () => void; onSettings: () => void; onAdd: () => void
}) {
  const listened = history.filter(entry => entry.item.kind === 'audio').map(entry => entry.item)
  const progress = useProgressOverview(listened.map(item => item.id), true)
  const resume = listened.filter(item => { const value = progress.get(item.id); return value && !value.completed && value.positionSeconds > 0 }).slice(0, 6)
  const recent = (resume.length ? resume : listened.slice(0, 3))
  const saved = library.filter(item => item.kind === 'audio')
  const discovery = gallery.sources.xiaoyuzhou
  const recommendations = (discovery.feed?.items ?? []).filter(item => item.kind === 'audio').slice(0, 12)
  const recommendationsEnabled = Boolean(gallery.enabled?.xiaoyuzhou)
  const summary = [
    !episodes.loaded ? '正在读取订阅更新' : episodes.entries.length ? `${episodes.entries.length} 条订阅更新` : '暂无订阅更新',
    `${saved.length} 集收藏`,
    resume.length ? `${resume.length} 集未听完` : '',
  ].filter(Boolean).join(' · ')
  return <div className="podcasts-page">
    <PageHeader title="播客" summary={summary} actions={<>
      <button type="button" className="button button-secondary" onClick={onSubscriptions}><Rss size={15}/>管理节目订阅</button>
      <button type="button" className="button button-primary" onClick={onAdd}><Plus size={15}/>添加单集</button>
    </>}/>
    {recent.length > 0 && <Section id="podcasts-resume" title={resume.length ? '继续收听' : '最近收听'} icon={<Headphones size={16} aria-hidden="true"/>}>
      <Episodes items={recent} shared={shared} status={item => supportsProgress(item.id) ? <CardProgress progress={progress.get(item.id)} supported/> : undefined}/>
    </Section>}
    <Section id="podcasts-updates" title="订阅节目更新" icon={<Rss size={16} aria-hidden="true"/>} count={episodes.entries.length} action={<button type="button" className="icon-button" aria-label="刷新订阅单集" title="刷新订阅单集" disabled={episodes.loading} onClick={() => void episodes.reload()}>{episodes.loading ? <LoaderCircle size={16} className="spin"/> : <RefreshCw size={16}/>}</button>}>
      {episodes.error && <div className="error-panel" role="alert"><p>{episodes.error}</p><button type="button" className="button button-secondary" onClick={() => void episodes.reload()}>重新读取</button></div>}
      {episodes.entries.length > 0 ? <Episodes items={episodes.entries.map(entry => entry.item)} shared={shared}/>
        : !episodes.loaded ? <SkeletonGallery count={3}/>
        : !episodes.error && <div className="podcast-empty"><p><strong>还没有订阅的节目单集</strong>订阅 RSS 播客后，更新时获取的单集会出现在这里。</p><button type="button" className="button button-secondary" onClick={onSubscriptions}>订阅播客</button></div>}
    </Section>
    <Section id="podcasts-discover" title="小宇宙推荐" icon={<Sparkles size={16} aria-hidden="true"/>} count={recommendations.length} action={recommendationsEnabled && <button type="button" className="icon-button" aria-label="刷新小宇宙推荐" title="刷新小宇宙推荐" disabled={discovery.loading} onClick={() => gallery.refresh(['xiaoyuzhou'])}>{discovery.loading ? <LoaderCircle size={16} className="spin"/> : <RefreshCw size={16}/>}</button>}>
      {!recommendationsEnabled ? <div className="podcast-empty"><p><strong>小宇宙推荐未开启</strong>在设置中登录小宇宙并开启推荐，就能在这里挑选单集。</p><button type="button" className="button button-secondary" onClick={onSettings}><Settings size={15}/>前往设置</button></div>
        : discovery.error && !recommendations.length ? <div className="error-panel" role="alert"><p>{discovery.error}</p><button type="button" className="button button-secondary" disabled={discovery.loading} onClick={() => void gallery.retry('xiaoyuzhou')}>重试</button></div>
        : recommendations.length ? <Episodes items={recommendations} shared={shared}/>
        : discovery.loading || !discovery.known ? <SkeletonGallery count={3}/>
        : <p className="field-hint">小宇宙暂时没有新的推荐。</p>}
    </Section>
    <Section id="podcasts-saved" title="收藏的单集" icon={<Bookmark size={16} aria-hidden="true"/>} count={saved.length}>
      {saved.length ? <Episodes items={saved} shared={shared}/> : <p className="field-hint">在任意单集上点击收藏，它会留在这里随时继续。</p>}
    </Section>
  </div>
}
