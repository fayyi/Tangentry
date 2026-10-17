import { AlertCircle, LoaderCircle, LogIn, RefreshCw, TvMinimal, X } from 'lucide-react'
import { useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { CardCollection } from '../../shared/ui/CardCollection'
import { gallerySources } from './useGallery'
import type { GallerySource, useGallery } from './useGallery'
import './discovery.css'
import { useRecommendationOrder } from './recommendationOrder'
import { request } from '../../shared/api/client'
import { useRecommendationExposure } from './useRecommendationExposure'
import { MediaFilters } from '../../shared/ui/MediaFilters'
import { SkeletonGallery } from '../../shared/ui/SkeletonLoading'
import { openDesktopPlatformLogin, useCapabilities, type PlatformWebView } from '../../shared/api/desktop'
import { useDiscoveryBatch } from './useDiscoveryBatch'
import { useDiscoveryTopics } from './useDiscoveryTopics'

const labels:Record<GallerySource,string>={subscriptions:'订阅来源',bilibili:'B站推荐',youtube:'YouTube 推荐',xiaohongshu:'小红书推荐',linuxdo:'Linux.do 话题',xiaoyuzhou:'小宇宙发现',douyin:'抖音推荐'}
const sourceKinds: Record<GallerySource, readonly string[]> = {
  subscriptions: ['video', 'article', 'image', 'pdf', 'audio'], bilibili: ['video'], youtube: ['video'], xiaohongshu: ['video', 'image'], linuxdo: ['article'], xiaoyuzhou: ['audio'], douyin: ['video'],
}
export function DiscoveryGallery({kind,onKind,origin,onOrigin,query,onClearQuery,notice,noticeError,gallery,savedIDs,pending,savingUnavailable,onSave,onRead,onDownload,onSearch,onSubscriptions,onBilibili}: {
  kind:string;onKind:(kind:string)=>void;origin:string;onOrigin:(origin:string)=>void;notice:string;noticeError:boolean
  query:string;onClearQuery:()=>void
  gallery:ReturnType<typeof useGallery>;savedIDs:Set<string>;pending:Set<string>;savingUnavailable:boolean
  onSave:(item:ContentItem)=>void;onRead:(item:ContentItem)=>void;onDownload:(item:ContentItem)=>void;onSearch:()=>void;onSubscriptions:()=>void;onBilibili:()=>void
}) {
  const capabilities = useCapabilities()
  const batchState = useDiscoveryBatch(origin !== 'subscriptions')
  const topicState = useDiscoveryTopics(origin !== 'subscriptions')
  const [platform, setPlatform] = useState('all')
  const [webViewPlatform, setWebViewPlatform] = useState<PlatformWebView | ''>('')
  const [webViewError, setWebViewError] = useState('')
  const matchesKind = (mediaKind: string) => kind === 'all' || mediaKind === kind
  const selectedSources = gallerySources.filter(source => (origin === 'subscriptions' ? source === 'subscriptions' : source !== 'subscriptions' && gallery.enabled?.[source]) &&
    (origin === 'subscriptions' || platform === 'all' || source === platform) &&
    (sourceKinds[source].some(matchesKind) || gallery.sources[source].feed?.items.some(item => matchesKind(item.kind))))
  const [published, setPublished] = useState(() => gallerySources.filter(source => source !== 'subscriptions').map(source => ({ source, feed: null as typeof gallery.sources[typeof source]['feed'], label: labels[source] })))
  const platformSources = selectedSources.filter(source => source !== 'subscriptions')
  const enabledPlatforms = gallerySources.filter(source => source !== 'subscriptions' && gallery.enabled?.[source])
  const settled = platformSources.every(source => gallery.sources[source].known && !gallery.sources[source].loading)
  let batch = published
  if (settled && platformSources.some(source => gallery.sources[source].feed !== published.find(entry => entry.source === source)?.feed)) {
    batch = published.map(entry => platformSources.includes(entry.source)
      ? { ...entry, feed: gallery.sources[entry.source].feed } : entry)
    setPublished(batch)
  }
  const ordered = useRecommendationOrder(origin === 'subscriptions'
    ? [{ source: 'subscriptions', feed: gallery.sources.subscriptions.feed, label: labels.subscriptions }]
    : batch.filter(entry => gallery.enabled?.[entry.source] && (platform === 'all' || entry.source === platform)), origin)
  const batchEntries = (batchState.batch?.items ?? []).map(entry => ({ item: entry.item, origin: `${labels[entry.source as GallerySource] ?? entry.source} · ${entry.reason}${entry.carriedOver ? ' · 延续未看' : ''}`, reason: entry.reason, carriedOver: entry.carriedOver }))
  const visibleEntries = origin === 'subscriptions' ? ordered : batchEntries
  const search = query.trim().toLocaleLowerCase()
  const items = visibleEntries.filter(({ item, origin: sourceLabel }) => matchesKind(item.kind) && (!search ||
    `${item.title} ${item.author} ${item.description} ${item.previewText || ''} ${item.platform} ${sourceLabel}`.toLocaleLowerCase().includes(search)))
  useRecommendationExposure(origin !== 'subscriptions', items.map(({ item }) => item.id).join(','))
  const loading = origin === 'subscriptions' ? selectedSources.some(source => gallery.sources[source].loading) : batchState.loading
  const continuationSources = selectedSources.filter(source => source !== 'subscriptions')
  const hasMore = origin === 'subscriptions' ? continuationSources.some(source => !gallery.sources[source].error && !gallery.sources[source].exhausted && gallery.sources[source].feed?.hasMore) : Boolean(batchState.batch?.hasNextBatch)
  const linuxdoOnly = enabledPlatforms.length === 1 && enabledPlatforms[0] === 'linuxdo'
  // Count sources with errors
  const errorSources = selectedSources.filter(source => gallery.sources[source].error)
  const batchErrors = Object.entries(batchState.batch?.sourceStatus ?? {}).filter(([, status]) => status !== 'ok' && status !== 'disabled')
  const hasErrors = errorSources.length > 0 || batchErrors.length > 0
  const needsVerification = (source: GallerySource) => (source === 'linuxdo' || source === 'douyin') && /重新验证|重新保存|重新导入|登录已失效|Cloudflare|验证页面|登录 Cookie|需要验证/i.test(gallery.sources[source].error)
  async function verifyPlatform(source: PlatformWebView) {
    if (webViewPlatform) return
    setWebViewPlatform(source); setWebViewError('')
    try {
      const before = await request<{ platforms: Array<{ platform: string; configured: boolean; updatedAt: number }> }>('/settings/cookies')
      const previous = before.platforms.find(value => value.platform === source)
      const previousUpdatedAt = previous?.updatedAt ?? 0
      await openDesktopPlatformLogin(source)
      // The native shell owns the completion lifecycle. Poll only the safe
      // status marker, then refresh recommendations after the WebView saves.
      let attempts = 0
      const poll = async () => {
        if (attempts++ >= 120) {
          setWebViewPlatform('')
          return
        }
        try {
          const result = await request<{ platforms: Array<{ platform: string; configured: boolean; updatedAt: number }> }>('/settings/cookies')
          const state = result.platforms.find(value => value.platform === source)
          if (state?.configured && (!previous?.configured || state.updatedAt > previousUpdatedAt)) {
            setWebViewPlatform('')
            await gallery.retry(source)
            return
          }
        } catch { /* The window may still be completing its challenge. */ }
        window.setTimeout(() => void poll(), 1500)
      }
      window.setTimeout(() => void poll(), 1500)
    } catch (cause) {
      setWebViewError(cause instanceof Error ? cause.message : '无法打开应用内登录窗口。')
      setWebViewPlatform('')
    }
  }
  const openRecommendation = (item: ContentItem) => {
    if (origin !== 'subscriptions' && ['bilibili', 'youtube', 'xiaohongshu', 'linuxdo', 'xiaoyuzhou', 'douyin'].includes(item.platform)) {
      void request(`/recommendations/${item.platform}/marks`, { method: 'PUT', keepalive: true, body: JSON.stringify({ contentId: item.id, opened: true }) }).catch(() => {})
    }
    onRead(item)
  }

  return <>
    <div className="discovery-toolbar discovery-home-toolbar">
      <div className="discovery-heading">
        <h1>发现</h1>
        <div className="gallery-source-switch" role="group" aria-label="内容来源"><button type="button" aria-label="平台推荐" aria-pressed={origin !== 'subscriptions'} onClick={() => onOrigin('platforms')}>推荐</button><button type="button" aria-label="订阅来源" aria-pressed={origin === 'subscriptions'} onClick={() => onOrigin('subscriptions')}>订阅来源</button></div>
      </div>
      <div className="discovery-toolbar-actions">
        {hasErrors && <details className="gallery-errors-summary"><summary aria-label={`${errorSources.length + batchErrors.length} 个来源异常`} title="来源状态"><AlertCircle size={14} /><span>{errorSources.length + batchErrors.length} 个来源异常</span></summary><div className="gallery-errors-popover">{errorSources.map(source => <div key={source}><strong>{labels[source]}</strong><p role="alert">{gallery.sources[source].error}</p><div className="gallery-error-actions"><button className="text-button" disabled={gallery.sources[source].loading} onClick={() => void gallery.retry(source)}>重试</button>{capabilities.platformWebViewLogin && needsVerification(source) && <button className="text-button" disabled={Boolean(webViewPlatform)} onClick={() => void verifyPlatform(source as PlatformWebView)}>{webViewPlatform === source ? <LoaderCircle size={14} className="spin" aria-hidden="true"/> : <LogIn size={14} aria-hidden="true"/>}{webViewPlatform === source ? '正在打开' : '重新验证'}</button>}</div></div>)}{batchErrors.map(([source, status]) => <div key={source}><strong>{labels[source as GallerySource] ?? source}</strong><p role="alert">{status === 'unavailable' ? '来源暂不可用，已保留其他来源内容。' : '来源获取失败，已保留其他来源内容。'}</p></div>)}{webViewError && <p className="gallery-errors-feedback" role="alert">{webViewError}</p>}</div></details>}
        <button type="button" className="icon-button" aria-label="打开B站专区" title="打开B站专区" onClick={onBilibili}><TvMinimal size={18}/></button><button type="button" className="icon-button" aria-label="刷新画廊内容" title="刷新画廊内容" disabled={loading || (origin === 'subscriptions' ? selectedSources.length === 0 : false)} onClick={() => origin === 'subscriptions' ? gallery.refresh(selectedSources) : batchState.retry()}>{loading ? <LoaderCircle size={18} className="spin"/> : <RefreshCw size={18}/>}</button>
      </div>
      <div className="discovery-filter-rail" role="group" aria-label="筛选发现内容" tabIndex={0}>
        <MediaFilters value={kind} onChange={onKind}/>
        {origin !== 'subscriptions' && <div className="discovery-batch-controls" role="group" aria-label="发现批次设置">
          <select aria-label="发现视角" value={batchState.options.view} onChange={event => batchState.setOptions({ view: event.target.value as typeof batchState.options.view })}><option value="wander">漫游</option><option value="latest">最新更新</option><option value="topic">按主题</option><option value="random">随便看看</option></select>
          <select aria-label="批次密度" value={batchState.options.size} onChange={event => batchState.setOptions({ size: Number(event.target.value) as 10 | 15 | 25 })}><option value="10">舒缓 · 10 条</option><option value="15">普通 · 15 条</option><option value="25">丰富 · 25 条</option></select>
          {batchState.options.view === 'topic' && <><select aria-label="选择主题" value={batchState.options.topic} onChange={event => batchState.setOptions({ topic: event.target.value })}><option value="">选择主题</option>{topicState.topics.map(topic => <option key={topic.id} value={topic.id}>{topic.name}</option>)}</select><input aria-label="新主题名称" value={batchState.options.topic && !topicState.topics.some(topic => topic.id === batchState.options.topic) ? batchState.options.topic : ''} placeholder="新主题关键词" onChange={event => batchState.setOptions({ topic: event.target.value })}/><button type="button" className="text-button" onClick={() => { const name = batchState.options.topic.trim(); if (name) void topicState.create(name) }}>保存主题</button>{topicState.topics.length > 0 && <div className="discovery-topic-list" aria-label="已保存主题">{topicState.topics.map(topic => <span key={topic.id} className="discovery-topic-chip">{topic.name}<button type="button" aria-label={`删除主题 ${topic.name}`} title={`删除主题 ${topic.name}`} onClick={() => void topicState.remove(topic)}><X size={12}/></button></span>)}</div>}</>}<label className="discovery-range"><span>陌生 {batchState.options.explorationRatio}%</span><input aria-label="陌生探索比例" type="range" min="0" max="30" step="5" value={batchState.options.explorationRatio} onChange={event => batchState.setOptions({ explorationRatio: Number(event.target.value) })}/></label>
        </div>}
        {origin !== 'subscriptions' && enabledPlatforms.length > 1 && <div className="discovery-platforms" role="group" aria-label="推荐平台">
          <button type="button" aria-pressed={platform === 'all'} onClick={() => { setPlatform('all'); batchState.setOptions({ source: '' }) }}>全部平台</button>
          {enabledPlatforms.map(source => <button key={source} type="button" data-platform={source} aria-pressed={platform === source} data-state={gallery.sources[source].error ? 'error' : gallery.sources[source].loading ? 'loading' : undefined} onClick={() => { const selected = platform === source ? 'all' : source; setPlatform(selected); batchState.setOptions({ source: selected === 'all' ? '' : selected }) }}><span className="platform-dot" aria-hidden="true"/>{labels[source].replace(/ ?(推荐|话题|发现)$/, '')}{gallery.sources[source].error && <span className="visually-hidden">（异常）</span>}</button>)}
        </div>}
        {origin !== 'subscriptions' && (platform === 'linuxdo' || linuxdoOnly) && <div className="discovery-platforms linuxdo-filters" role="group" aria-label="Linux.do 话题分类">
          {([['latest', '最新'], ['top', '热门'], ['new', '新话题'], ['unread', '未读']] as const).map(([value, label]) => <button key={value} type="button" aria-pressed={gallery.linuxdoFilter === value} onClick={() => gallery.setLinuxdoFilter(value)}>{label}</button>)}
        </div>}
        <span className="content-count">{items.length} 条内容</span>
      </div>
    </div>
    {notice&&<p className="inline-feedback collection-save-feedback" role={noticeError?'alert':'status'}>{notice}</p>}
    {savingUnavailable&&<p className="field-hint" role="status">资料库暂不可用，收藏操作等待重新连接。</p>}
    {search && <p className="discovery-search-result" role="status">当前已加载内容中找到 {items.length} 条结果<button type="button" className="text-button" onClick={onClearQuery}>清除搜索</button></p>}
    {items.length>0?<CardCollection masonry={!['video', 'audio'].includes(kind)} rowGap={20} data-media={kind} className="mixed-gallery" aria-label="发现内容">{items.map(({item,origin})=><ModernGalleryCard key={item.id} discovery item={item} origin={origin} saved={savedIDs.has(item.id)} pending={savingUnavailable||pending.has(item.id)} onRead={openRecommendation} onSave={onSave} onDownload={onDownload}/>)}</CardCollection>:loading ? <SkeletonGallery count={8}/> : search ? <section className="empty-state"><h2>没有找到匹配的内容</h2><p>试试其他关键词，或清除搜索查看当前内容。</p><button type="button" className="button button-secondary" onClick={onClearQuery}>查看全部内容</button></section> : <section className="empty-state"><h2>{origin === 'subscriptions' ? '暂无订阅内容' : '这里还没有推荐内容'}</h2><p>{origin === 'subscriptions' ? '添加订阅后，你关注的内容会出现在这里。' : '添加一个内容链接，或在设置中连接你使用的平台。'}</p><button className="button button-primary" onClick={origin === 'subscriptions' ? onSubscriptions : onSearch}>{origin === 'subscriptions' ? '管理订阅' : '添加内容'}</button>{platform !== 'all' && <button className="text-button" onClick={() => setPlatform('all')}>查看全部平台</button>}</section>}
    {origin !== 'subscriptions' && !search && <div className="gallery-load-more discovery-batch-end" aria-live="polite">{batchState.error ? <><span role="alert">{batchState.error}</span><button className="button button-secondary" onClick={batchState.retry}><RefreshCw size={15}/>重试</button></> : loading ? <><LoaderCircle size={15} className="spin"/>正在准备这一批…</> : hasMore ? <><span>这一批已显示 {items.length} 条</span><button className="button button-primary" onClick={batchState.nextBatch}>再看一批</button><button className="button button-secondary" onClick={() => batchState.setOptions({ view: 'random' })}>围绕刚才探索</button></> : items.length ? <><span>{batchState.batch?.endReason ?? '本轮已结束'}</span><button className="button button-secondary" onClick={() => batchState.setOptions({ view: 'random' })}>围绕刚才探索</button><button className="text-button" onClick={onSubscriptions}>先到这里，去看订阅</button></> : null}</div>}
  </>
}
