import { AlertCircle, Edit3, LoaderCircle, LogIn, Plus, RefreshCw, TvMinimal, UserRound, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ContentItem, DiscoveryTopicRule } from '../../shared/api/types'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { CardCollection } from '../../shared/ui/CardCollection'
import { gallerySources } from './useGallery'
import type { GallerySource, useGallery } from './useGallery'
import './discovery.css'
import { request } from '../../shared/api/client'
import { useRecommendationExposure } from './useRecommendationExposure'
import { MediaFilters } from '../../shared/ui/MediaFilters'
import { SkeletonGallery } from '../../shared/ui/SkeletonLoading'
import { openDesktopPlatformLogin, useCapabilities, type PlatformWebView } from '../../shared/api/desktop'
import { discoveryReasonType, getDiscoverySourceLabel, readDiscoveryFilterState, updateDiscoveryFilterState, useDiscoveryBatch } from './useDiscoveryBatch'
import { useDiscoveryTopics } from './useDiscoveryTopics'
import { feedbackMessage, type DiscoveryFeedbackTarget, type DiscoveryFeedbackType } from './discoveryFeedback'

const labels: Record<GallerySource, string> = { subscriptions: '订阅更新', bilibili: 'B站推荐', youtube: 'YouTube 推荐', xiaohongshu: '小红书推荐', linuxdo: 'Linux.do 话题', xiaoyuzhou: '小宇宙发现', douyin: '抖音推荐' }

export function DiscoveryGallery({ kind, onKind, query, onClearQuery, notice, noticeError, gallery, savedIDs, pending, savingUnavailable, onSave, onRead, onDownload, onCreator, onSearch, onSubscriptions, onBilibili }: {
  kind: string; onKind: (kind: string) => void; origin: string; onOrigin: (origin: string) => void; notice: string; noticeError: boolean
  query: string; onClearQuery: () => void
  gallery: ReturnType<typeof useGallery>; savedIDs: Set<string>; pending: Set<string>; savingUnavailable: boolean
  onSave: (item: ContentItem) => void; onRead: (item: ContentItem) => void; onDownload: (item: ContentItem) => void; onCreator: (platform: string, authorID: string) => void; onSearch: () => void; onSubscriptions: () => void; onBilibili: () => void
}) {
  const capabilities = useCapabilities()
  const batchState = useDiscoveryBatch(true)
  const topicState = useDiscoveryTopics(true)
  const sentinel = useRef<HTMLDivElement>(null)
  const [platform, setPlatformState] = useState(() => readDiscoveryFilterState().platform)
  const [topic, setTopicState] = useState(() => readDiscoveryFilterState().topic)
  const [webViewPlatform, setWebViewPlatform] = useState<PlatformWebView | ''>('')
  const [webViewError, setWebViewError] = useState('')
  const [feedbackHidden, setFeedbackHidden] = useState(() => new Set<string>())
  const [undoFeedback, setUndoFeedback] = useState<{ id: string; itemID: string; message: string } | null>(null)
  const [feedbackError, setFeedbackError] = useState('')
  const matchesKind = (mediaKind: string) => kind === 'all' || mediaKind === kind
  const sourceOptions = gallerySources.filter(source => source === 'subscriptions' || gallery.enabled?.[source] !== false)
  const sourceStatus = batchState.batch?.sourceStatus ?? {}
  const selectedEntries = (batchState.batch?.items ?? []).filter(entry => platform === 'all' || entry.source === platform)
  const search = query.trim().toLocaleLowerCase()
  const items = selectedEntries.filter(({ item, reason }) => !feedbackHidden.has(item.id) && matchesKind(item.kind) && (!search || `${item.title} ${item.author} ${item.description} ${item.previewText || ''} ${item.platform} ${reason}`.toLocaleLowerCase().includes(search)))
  useRecommendationExposure(true, items.map(({ item }) => item.id).join(','))
  const loading = batchState.loading
  const errorCount = Object.values(sourceStatus).filter(status => status.status !== 'ok' && status.status !== 'disabled').length
  const setPlatform = (value: string) => { setPlatformState(value); updateDiscoveryFilterState({ platform: value }) }
  const setTopic = (value: string) => { setTopicState(value); updateDiscoveryFilterState({ topic: value }) }
  const setKind = (value: string) => { onKind(value); updateDiscoveryFilterState({ kind: value }) }

  useEffect(() => {
    if (!sentinel.current || !batchState.batch?.hasNextBatch) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting) && !batchState.loading) batchState.nextBatch() }, { rootMargin: '720px 0px' })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [batchState.batch?.hasNextBatch, batchState.loading, batchState.nextBatch])

  async function verifyPlatform(source: PlatformWebView) {
    if (webViewPlatform) return
    setWebViewPlatform(source); setWebViewError('')
    try {
      const before = await request<{ platforms: Array<{ platform: string; configured: boolean; updatedAt: number }> }>('/settings/cookies')
      const previous = before.platforms.find(value => value.platform === source)
      await openDesktopPlatformLogin(source)
      let attempts = 0
      const poll = async () => {
        if (attempts++ >= 120) { setWebViewPlatform(''); return }
        try {
          const result = await request<{ platforms: Array<{ platform: string; configured: boolean; updatedAt: number }> }>('/settings/cookies')
          const state = result.platforms.find(value => value.platform === source)
          if (state?.configured && (!previous?.configured || state.updatedAt > (previous.updatedAt ?? 0))) { setWebViewPlatform(''); batchState.retry(); return }
        } catch { /* The native window may still be completing its challenge. */ }
        window.setTimeout(() => void poll(), 1500)
      }
      window.setTimeout(() => void poll(), 1500)
    } catch (cause) { setWebViewError(cause instanceof Error ? cause.message : '无法打开应用内登录窗口。'); setWebViewPlatform('') }
  }

  const manageTopic = (existing?: DiscoveryTopicRule) => {
    const value = window.prompt(existing ? '编辑主题' : '新增主题', existing?.name ?? '')?.trim()
    if (!value) return
    if (existing) void topicState.update(existing, value)
    else void topicState.create(value)
  }
  const openRecommendation = (item: ContentItem) => {
    if (['bilibili', 'youtube', 'xiaohongshu', 'linuxdo', 'xiaoyuzhou', 'douyin'].includes(item.platform)) void request(`/recommendations/${item.platform}/marks`, { method: 'PUT', keepalive: true, body: JSON.stringify({ contentId: item.id, opened: true }) }).catch(() => {})
    onRead(item)
  }
  async function sendFeedback(type: DiscoveryFeedbackType, target: DiscoveryFeedbackTarget, value: string, item: ContentItem) {
    try {
      setFeedbackError('')
      const saved = await request<{ id: string }>('/discovery/feedback', { method: 'POST', body: JSON.stringify({ type, target, value }) })
      if (type === 'hide') setFeedbackHidden(current => new Set(current).add(item.id))
      if (type === 'block_creator') setFeedbackHidden(current => new Set([...current, ...selectedEntries.filter(entry => (entry.item.authorId || entry.item.author) === value).map(entry => entry.item.id)]))
      setUndoFeedback({ id: saved.id, itemID: item.id, message: feedbackMessage(type) })
      if (type === 'more' || type === 'less' || type === 'block_creator') batchState.retry()
    } catch (cause) { setFeedbackError(cause instanceof Error ? cause.message : '发现反馈未能保存，请重试。') }
  }
  async function undoFeedbackAction() {
    if (!undoFeedback) return
    const current = undoFeedback
    try {
      await request(`/discovery/feedback/${encodeURIComponent(current.id)}`, { method: 'DELETE' })
      setFeedbackHidden(previous => { const next = new Set(previous); next.delete(current.itemID); return next })
      setUndoFeedback(null)
      batchState.retry()
    } catch (cause) { setFeedbackError(cause instanceof Error ? cause.message : '撤销失败，请到设置中重试。') }
  }

  return <>
    <div className="discovery-toolbar discovery-home-toolbar">
      <div className="discovery-heading"><h1>发现</h1></div>
      <div className="discovery-toolbar-actions">
        {errorCount > 0 && <details className="gallery-errors-summary"><summary aria-label={`${errorCount} 个来源异常`} title="来源状态"><AlertCircle size={14} /><span>{errorCount} 个来源异常</span></summary><div className="gallery-errors-popover">{Object.entries(sourceStatus).filter(([, status]) => status.status !== 'ok' && status.status !== 'disabled').map(([source, status]) => <div key={source}><strong>{labels[source as GallerySource] ?? source}</strong><p role="alert">{status.message || (status.status === 'unavailable' ? '来源暂不可用，已保留其他来源内容。' : '来源获取失败，已保留其他来源内容。')}</p>{capabilities.platformWebViewLogin && status.requiresVerification && (source === 'linuxdo' || source === 'douyin') && <button className="text-button" disabled={Boolean(webViewPlatform)} onClick={() => void verifyPlatform(source as PlatformWebView)}>{webViewPlatform === source ? <LoaderCircle size={14} className="spin" /> : <LogIn size={14} />}{webViewPlatform === source ? '正在打开' : '重新验证'}</button>}</div>)}{webViewError && <p className="gallery-errors-feedback" role="alert">{webViewError}</p>}<a className="text-button gallery-health-link" href="/subscriptions#health">管理来源健康</a></div></details>}
        <button type="button" className="icon-button" aria-label="打开B站专区" title="打开B站专区" onClick={onBilibili}><TvMinimal size={18} /></button>
        <button type="button" className="icon-button" aria-label="刷新发现内容" title="刷新发现内容" disabled={loading} onClick={batchState.retry}>{loading ? <LoaderCircle size={18} className="spin" /> : <RefreshCw size={18} />}</button>
      </div>
      <div className="discovery-filter-rail" role="group" aria-label="筛选发现内容" tabIndex={0}>
        <MediaFilters value={kind} onChange={setKind} />
        <div className="discovery-platforms" role="group" aria-label="内容来源"><button type="button" aria-pressed={platform === 'all'} onClick={() => setPlatform('all')}>全部</button>{sourceOptions.map(source => <button key={source} type="button" data-platform={source} aria-pressed={platform === source} onClick={() => setPlatform(platform === source ? 'all' : source)}><span className="platform-dot" aria-hidden="true" />{labels[source]}</button>)}</div>
        <div className="discovery-topics" role="group" aria-label="主题筛选"><button type="button" aria-pressed={!topic} onClick={() => { setTopic(''); batchState.setOptions({ topic: '' }) }}>全部主题</button>{topicState.topics.map(value => <span className="discovery-topic-chip" key={value.id}><button type="button" aria-pressed={topic === value.id} onClick={() => { setTopic(value.id); batchState.setOptions({ topic: value.id }) }}>{value.name}</button><button type="button" className="discovery-topic-action" aria-label={`编辑主题 ${value.name}`} title="编辑主题" onClick={() => manageTopic(value)}><Edit3 size={12} /></button><button type="button" className="discovery-topic-action" aria-label={`删除主题 ${value.name}`} title="删除主题" onClick={() => void topicState.remove(value)}><X size={12} /></button></span>)}<button type="button" className="discovery-topic-add" aria-label="新增主题" title="新增主题" onClick={() => manageTopic()}><Plus size={15} /></button></div>
        <span className="content-count">{items.length} 条内容</span>
      </div>
    </div>
    {batchState.optionsStale && <p className="discovery-new-content" role="status">有新内容，点击刷新<button type="button" className="text-button" onClick={batchState.retry}>刷新</button></p>}
    {undoFeedback && <p className="inline-feedback collection-save-feedback" role="status">{undoFeedback.message}<button type="button" className="text-button" onClick={() => void undoFeedbackAction()}>撤销</button></p>}
    {feedbackError && <p className="inline-feedback" role="alert">{feedbackError}</p>}
    {notice && <p className="inline-feedback collection-save-feedback" role={noticeError ? 'alert' : 'status'}>{notice}</p>}
    {savingUnavailable && <p className="field-hint" role="status">资料库暂不可用，收藏操作等待重新连接。</p>}
    {search && <p className="discovery-search-result" role="status">当前已加载内容中找到 {items.length} 条结果<button type="button" className="text-button" onClick={onClearQuery}>清除搜索</button></p>}
    {items.length > 0 ? <CardCollection masonry={!['video', 'audio'].includes(kind)} rowGap={20} data-media={kind} className="mixed-gallery" aria-label="发现内容">{items.map(({ item, source, reason, carriedOver }) => <div className="discovery-card-with-creator" key={item.id}><ModernGalleryCard discovery item={item} origin={getDiscoverySourceLabel(source, reason, carriedOver)} discoveryReason={reason} discoveryType={discoveryReasonType(source, reason, carriedOver)} saved={savedIDs.has(item.id)} pending={savingUnavailable || pending.has(item.id)} onRead={openRecommendation} onSave={onSave} onDownload={onDownload} onFeedback={(type, target, value) => void sendFeedback(type, target, value, item)} />{item.authorId && <button type="button" className="creator-link" onClick={() => onCreator(item.platform, item.authorId)}><UserRound size={13}/>看看 TA 最近</button>}</div>)}</CardCollection> : loading ? <SkeletonGallery count={8} /> : search ? <section className="empty-state"><h2>没有找到匹配的内容</h2><p>试试其他关键词，或清除搜索查看当前内容。</p><button type="button" className="button button-secondary" onClick={onClearQuery}>查看全部内容</button></section> : <section className="empty-state"><h2>这里还没有推荐内容</h2><p>添加一个内容链接，或在设置中连接你使用的平台。</p><button className="button button-primary" onClick={onSearch}>添加内容</button><button className="text-button" onClick={onSubscriptions}>管理订阅</button></section>}
    <div ref={sentinel} className="gallery-load-more discovery-feed-status" aria-live="polite">{batchState.error ? <><span role="alert">{batchState.error}</span><button className="button button-secondary" onClick={batchState.retry}><RefreshCw size={15} />重试</button></> : loading ? <><LoaderCircle size={15} className="spin" />正在加载更多…</> : batchState.batch?.hasNextBatch ? <span>继续下滑加载更多</span> : items.length ? <><span>暂时没有更多了</span><button className="button button-secondary" onClick={batchState.retry}><RefreshCw size={15} />刷新</button></> : null}</div>
  </>
}
