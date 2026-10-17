import { AlertCircle, LoaderCircle, LogIn, RefreshCw, TvMinimal } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
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
  const search = query.trim().toLocaleLowerCase()
  const items = ordered.filter(({ item, origin: sourceLabel }) => matchesKind(item.kind) && (!search ||
    `${item.title} ${item.author} ${item.description} ${item.previewText || ''} ${item.platform} ${sourceLabel}`.toLocaleLowerCase().includes(search)))
  useRecommendationExposure(origin !== 'subscriptions', items.map(({ item }) => item.id).join(','))
  const loading=selectedSources.some(source=>gallery.sources[source].loading)
  const loadMore = gallery.loadMore
  const continuationSources = selectedSources.filter(source => source !== 'subscriptions')
  const continuationKey = continuationSources.join(',')
  const loadingMore = continuationSources.some(source => gallery.sources[source].loading && gallery.sources[source].feed?.hasMore)
  const sentinel = useRef<HTMLDivElement>(null)
  const hasMore = continuationSources.some(source => !gallery.sources[source].error && !gallery.sources[source].exhausted && gallery.sources[source].feed?.hasMore)
  const linuxdoOnly = enabledPlatforms.length === 1 && enabledPlatforms[0] === 'linuxdo'
  useEffect(() => {
    if (origin === 'subscriptions' || search || !hasMore || loading) return
    const node = sentinel.current
    if (!node) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) void loadMore(continuationKey.split(',') as GallerySource[]) }, { rootMargin: '720px 0px' })
    observer.observe(node)
    return () => observer.disconnect()
  }, [origin, search, hasMore, loading, loadMore, continuationKey])

  // Count sources with errors
  const errorSources = selectedSources.filter(source => gallery.sources[source].error)
  const hasErrors = errorSources.length > 0
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
  const pausedSources = continuationSources.filter(source => gallery.enabled?.[source] && gallery.sources[source].feed?.hasMore &&
    (gallery.sources[source].error || gallery.sources[source].exhausted))
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
        {hasErrors && <details className="gallery-errors-summary"><summary aria-label={`${errorSources.length} 个来源异常`} title={`${errorSources.length} 个来源异常`}><AlertCircle size={14} /><span>{errorSources.length} 个来源异常</span></summary><div className="gallery-errors-popover">{errorSources.map(source => <div key={source}><strong>{labels[source]}</strong><p role="alert">{gallery.sources[source].error}</p><div className="gallery-error-actions"><button className="text-button" disabled={gallery.sources[source].loading} onClick={() => void gallery.retry(source)}>重试</button>{capabilities.platformWebViewLogin && needsVerification(source) && <button className="text-button" disabled={Boolean(webViewPlatform)} onClick={() => void verifyPlatform(source as PlatformWebView)}>{webViewPlatform === source ? <LoaderCircle size={14} className="spin" aria-hidden="true"/> : <LogIn size={14} aria-hidden="true"/>}{webViewPlatform === source ? '正在打开' : '重新验证'}</button>}</div></div>)}{webViewError && <p className="gallery-errors-feedback" role="alert">{webViewError}</p>}<a className="text-button gallery-health-link" href="/subscriptions#health">管理来源健康</a></div></details>}
        <button type="button" className="icon-button" aria-label="打开B站专区" title="打开B站专区" onClick={onBilibili}><TvMinimal size={18}/></button><button type="button" className="icon-button" aria-label="刷新画廊内容" title="刷新画廊内容" disabled={loading || selectedSources.length === 0} onClick={() => gallery.refresh(selectedSources)}>{loading ? <LoaderCircle size={18} className="spin"/> : <RefreshCw size={18}/>}</button>
      </div>
      <div className="discovery-filter-rail" role="group" aria-label="筛选发现内容" tabIndex={0}>
        <MediaFilters value={kind} onChange={onKind}/>
        {origin !== 'subscriptions' && enabledPlatforms.length > 1 && <div className="discovery-platforms" role="group" aria-label="推荐平台">
          <button type="button" aria-pressed={platform === 'all'} onClick={() => setPlatform('all')}>全部平台</button>
          {enabledPlatforms.map(source => <button key={source} type="button" data-platform={source} aria-pressed={platform === source} data-state={gallery.sources[source].error ? 'error' : gallery.sources[source].loading ? 'loading' : undefined} onClick={() => setPlatform(platform === source ? 'all' : source)}><span className="platform-dot" aria-hidden="true"/>{labels[source].replace(/ ?(推荐|话题|发现)$/, '')}{gallery.sources[source].error && <span className="visually-hidden">（异常）</span>}</button>)}
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
    {origin !== 'subscriptions' && !search && <div ref={sentinel} className="gallery-load-more" aria-live="polite">{loading ? <><LoaderCircle size={15} className="spin"/>{loadingMore ? '正在加载更多推荐…' : '正在汇集推荐…'}</> : pausedSources.length ? <button className="button button-secondary" onClick={() => { for (const source of pausedSources) void gallery.retry(source) }}><RefreshCw size={15}/>{pausedSources.some(source => gallery.sources[source].error) ? '重试加载更多' : '继续获取推荐'}</button> : hasMore ? '继续下滑加载更多' : items.length ? '平台暂未提供更多推荐' : null}</div>}
  </>
}
