import { ArrowLeft, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem, Feed, SearchPage, UnifiedSearchResponse } from '../../shared/api/types'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { CardCollection } from '../../shared/ui/CardCollection'
import { appendSearchItems, buildSearchParams, creatorPlatformSupport, groupSearchResults, mergeSearchItems, type SearchGroup, type SearchMode } from './searchLogic'

const labels: Record<string, string> = { bilibili: 'B站', youtube: 'YouTube', linuxdo: 'Linux.do', xiaoyuzhou: '小宇宙', xiaohongshu: '小红书', douyin: '抖音', local: '本机资料库与订阅' }
type TimeFilter = '' | 'day' | 'week' | 'month'
type Props = { initialQuery?: string; savedIDs: Set<string>; pending: Set<string>; onRead: (item: ContentItem) => void; onSave: (item: ContentItem) => void; onCreator: (platform: string, authorID: string) => void; onSubscribe: (platform: string, authorID: string) => void }

function timeBounds(value: TimeFilter): { from?: number; to?: number } {
  if (!value) return {}
  const to = Math.floor(Date.now() / 1000)
  return { from: to - (value === 'day' ? 86400 : value === 'week' ? 7 * 86400 : 30 * 86400), to }
}

export function UnifiedSearchPage({ initialQuery = '', savedIDs, pending, onRead, onSave, onCreator, onSubscribe }: Props) {
  const [query, setQuery] = useState(initialQuery)
  const [platform, setPlatform] = useState('')
  const [kind, setKind] = useState('')
  const [time, setTime] = useState<TimeFilter>('')
  const [mode, setMode] = useState<SearchMode>('grouped')
  const [result, setResult] = useState<UnifiedSearchResponse | null>(null)
  const [loadingMore, setLoadingMore] = useState<Set<string>>(new Set())
  const creator = new URLSearchParams(location.search).get('creator')
  const creatorPlatform = new URLSearchParams(location.search).get('creatorPlatform')
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const groups = useMemo(() => result ? groupSearchResults(result, labels) : [], [result])
  const mixed = useMemo(() => mergeSearchItems(groups), [groups])

  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => { setQuery(initialQuery); if (initialQuery.trim()) void search(initialQuery) }, [initialQuery])
  useEffect(() => { if (result && query.trim() && !creator) void search(query) }, [platform, kind, time])

  async function search(nextQuery = query) {
    const value = nextQuery.trim()
    if (!value) return
    controller.current?.abort()
    const next = new AbortController(); controller.current = next; setError('')
    try {
      const linkSource = value.match(/bilibili\.com|b23\.tv|youtube\.com|youtu\.be|linux\.do|xiaoyuzhoufm\.com|xiaohongshu\.com|xhslink\.com|douyin\.com/i)
      if (linkSource) {
        const source = /bilibili|b23\.tv/i.test(value) ? 'bilibili' : /youtube|youtu\.be/i.test(value) ? 'youtube' : /linux\.do/i.test(value) ? 'linuxdo' : /xiaoyuzhou/i.test(value) ? 'xiaoyuzhou' : /xiaohongshu|xhslink/i.test(value) ? 'xiaohongshu' : 'douyin'
        const feed = await request<Feed>(`/discovery/${source}?q=${encodeURIComponent(value)}`, { signal: next.signal })
        setResult({ query: value, local: { items: [], total: 0, counts: {} }, sources: { [source]: { page: { ...feed, platform: source, cursor: undefined } } }, link: feed }); return
      }
      const bounds = timeBounds(time)
      const params = buildSearchParams(value, { platform, kind, ...bounds })
      setResult(await request<UnifiedSearchResponse>(`/search?${params}`, { signal: next.signal }))
    } catch (cause) { if (!next.signal.aborted) setError(cause instanceof Error ? cause.message : '搜索未能完成。') }
  }

  async function loadMore(group: SearchGroup) {
    const source = result?.sources[group.key]; const cursor = source?.page.cursor
    if (!result || !cursor || loadingMore.has(group.key) || group.key === 'local') return
    setLoadingMore(current => new Set(current).add(group.key))
    try {
      const bounds = timeBounds(time)
      const params = buildSearchParams(result.query, { platform: group.key, cursor, kind, ...bounds })
      const page = await request<UnifiedSearchResponse>(`/search?${params}`); const incoming = page.sources[group.key]?.page
      if (incoming) setResult(current => { if (!current) return page; const previous = current.sources[group.key]?.page; const merged: SearchPage = { ...incoming, items: appendSearchItems(previous?.items ?? [], incoming.items) }; return { ...current, sources: { ...current.sources, [group.key]: { page: merged, error: page.sources[group.key]?.error } } } })
    } catch (cause) { setError(cause instanceof Error ? cause.message : '加载更多结果失败。') }
    finally { setLoadingMore(current => { const next = new Set(current); next.delete(group.key); return next }) }
  }

  async function loadCreatorMore() {
    if (!result || !creator || !creatorPlatform || loadingMore.has(creatorPlatform)) return
    const page = result.sources[creatorPlatform]?.page.cursor
    if (!page) return
    setLoadingMore(current => new Set(current).add(creatorPlatform))
    try {
      const feed = await request<SearchPage>(`/search/creator/${creatorPlatform}/${encodeURIComponent(creator)}?cursor=${encodeURIComponent(page)}`)
      setResult(current => {
        if (!current) return current
        const previous = current.sources[creatorPlatform]?.page
        const merged: SearchPage = { ...previous, ...feed, platform: creatorPlatform, items: appendSearchItems(previous?.items ?? [], feed.items) }
        return { ...current, sources: { ...current.sources, [creatorPlatform]: { page: merged } } }
      })
    } catch (cause) { setError(cause instanceof Error ? cause.message : '加载创作者内容失败。') }
    finally { setLoadingMore(current => { const next = new Set(current); next.delete(creatorPlatform); return next }) }
  }

  useEffect(() => {
    if (!creator || !creatorPlatform) return
    if (!creatorPlatformSupport(creatorPlatform).view) { setError(`${labels[creatorPlatform] ?? creatorPlatform} 暂时不支持创作者临时视图。`); return }
    const next = new AbortController(); controller.current = next
    request<SearchPage>(`/search/creator/${creatorPlatform}/${encodeURIComponent(creator)}`, { signal: next.signal }).then(feed => setResult({ query: creator, local: { items: [], total: 0, counts: {} }, sources: { [creatorPlatform]: { page: feed } } })).catch(cause => { if (!next.signal.aborted) setError(cause instanceof Error ? cause.message : '暂时无法读取创作者内容。') })
    return () => next.abort()
  }, [creator, creatorPlatform])

  function card(item: ContentItem) { return <ModernGalleryCard item={item} saved={savedIDs.has(item.id)} pending={pending.has(item.id)} onRead={onRead} onSave={onSave} onCreator={item.authorId ? value => onCreator(value.platform, value.authorId) : undefined} /> }

  if (creator && creatorPlatform) {
    const support = creatorPlatformSupport(creatorPlatform)
    return <section className="creator-temporary-view"><button type="button" className="button button-secondary" onClick={() => history.back()}><ArrowLeft size={16}/>返回搜索</button><div className="results-heading"><div><strong>{labels[creatorPlatform] ?? creatorPlatform} 创作者最近内容</strong>{!support.subscribe && <p className="field-hint">该平台暂不支持一键订阅；这是一次性的临时查看，不会写入订阅或推荐偏好。</p>}</div>{support.subscribe && <button type="button" className="button button-primary" onClick={() => onSubscribe(creatorPlatform, creator)}>订阅</button>}</div>{error && <div className="error-panel" role="alert">{error}</div>}{groups.map(group => <div key={group.key}><CardCollection masonry className="modern-gallery">{group.items.map(card)}</CardCollection>{result?.sources[group.key]?.page.hasMore && result.sources[group.key].page.cursor && <button type="button" className="button button-secondary" onClick={() => void loadCreatorMore()} disabled={loadingMore.has(group.key)}>{loadingMore.has(group.key) ? '正在加载…' : '查看更多'}</button>}</div>)}</section>
  }

  return <>
    <section className="unified-search-head"><h1>统一搜索</h1><form className="search-field unified-search-form" role="search" onSubmit={event => { event.preventDefault(); void search() }}><Search size={17} aria-hidden="true"/><input autoFocus type="search" aria-label="搜索本机资料库与各平台" placeholder="搜索标题、作者、正文或字幕" value={query} onChange={event => setQuery(event.target.value)} /><button type="submit" className="button button-primary">搜索</button></form><div className="unified-search-filters"><label>平台<select value={platform} onChange={event => setPlatform(event.target.value)}><option value="">全部平台</option>{Object.entries(labels).filter(([key]) => key !== 'local').map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label><label>媒体<select value={kind} onChange={event => setKind(event.target.value)}><option value="">全部类型</option><option value="video">视频</option><option value="audio">音频</option><option value="article">文章</option><option value="image">图文</option><option value="pdf">PDF</option></select></label><label>时间<select value={time} onChange={event => setTime(event.target.value as TimeFilter)}><option value="">全部时间</option><option value="day">最近一天</option><option value="week">最近一周</option><option value="month">最近一个月</option></select></label><div className="segmented" role="group" aria-label="结果显示方式"><button type="button" aria-pressed={mode === 'grouped'} onClick={() => setMode('grouped')}>按平台</button><button type="button" aria-pressed={mode === 'mixed'} onClick={() => setMode('mixed')}>混合排序</button></div></div></section>
    {error && <div className="error-panel" role="alert">{error}</div>}{result && groups.length === 0 && <section className="empty-state"><h2>没有找到相关内容</h2><p>换个关键词，或粘贴链接导入内容。</p></section>}
    {mode === 'mixed' && result && <section className="unified-search-group"><div className="results-heading"><strong>混合排序</strong><span>{mixed.length} 条去重结果</span></div><CardCollection masonry className="modern-gallery">{mixed.map(card)}</CardCollection></section>}
    {mode === 'grouped' && groups.map(group => <section className="unified-search-group" key={group.key}><div className="results-heading"><strong>{group.title}</strong><span>{group.error || `${group.items.length} 条结果`}</span></div><CardCollection masonry className="modern-gallery">{group.items.slice(0, 8).map(card)}</CardCollection>{result?.sources[group.key]?.page.hasMore && result.sources[group.key].page.cursor && <button type="button" className="button button-secondary" onClick={() => void loadMore(group)} disabled={loadingMore.has(group.key)}>{loadingMore.has(group.key) ? '正在加载…' : '查看更多'}</button>}</section>)}
  </>
}
