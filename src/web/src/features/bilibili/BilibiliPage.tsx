import { ArrowLeft, Bookmark, ExternalLink, Flame, ListVideo, LoaderCircle, Radio, RefreshCw, Trophy, UserRound } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { CardCollection } from '../../shared/ui/CardCollection'
import type { BilibiliTab, LiveRoom, useBilibiliBrowse } from './useBilibiliBrowse'
import './bilibili.css'

const tabs = [
  ['popular', '热门', Flame], ['ranking', '排行', Trophy], ['dynamic', '动态', ListVideo], ['live', '直播', Radio], ['my', '我的', UserRound],
] as const
function LiveCard({ room }: { room: LiveRoom }) {
  const [failed, setFailed] = useState(false)
  return <article className="bilibili-live-card">
    <a href={room.url} target="_blank" rel="noopener noreferrer" aria-label={'观看直播 ' + room.title}>
      <div className="bilibili-live-cover">{room.thumbnailUrl && !failed ? <img src={room.thumbnailUrl} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)}/> : <Radio size={24} aria-hidden="true"/>}<span>直播中</span></div>
      <h2>{room.title}<ExternalLink size={13} aria-hidden="true"/></h2><p>{room.author}</p>
    </a>
  </article>
}

export function BilibiliPage({ browser, savedIDs, pending, unavailable, notice, noticeError, onRead, onSave, onReturn, onLogin, onLibrary }: {
  browser: ReturnType<typeof useBilibiliBrowse>; savedIDs: Set<string>; pending: Set<string>; unavailable: boolean; notice: string; noticeError: boolean
  onRead: (item: ContentItem) => void; onSave: (item: ContentItem) => void; onReturn: () => void; onLogin: () => void; onLibrary: () => void
}) {
  const { tab, setTab, state, profile } = browser
  const sentinel = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!sentinel.current || !state.page?.hasMore) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting) && !state.loading) browser.more() }, { rootMargin: '640px 0px' })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [browser, state.loading, state.page?.hasMore])
  const count = tab === 'live' ? state.page?.rooms?.length ?? 0 : state.page?.items.length ?? 0
  const viewLabel = (value: number) => value >= 10000 ? `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)} 万播放` : `${value} 播放`
  const empty: Record<BilibiliTab, string> = { popular: '暂无热门视频', ranking: '暂无排行内容', dynamic: '关注的 UP 主暂无视频动态', live: '这个分区暂时没有直播', my: '暂无稍后再看' }
  return <section className="bilibili-page" aria-label="B站专区">
    <div className="bilibili-heading">
      <button className="icon-button" title="返回发现" aria-label="返回发现" onClick={onReturn}><ArrowLeft size={18}/></button>
      <h1>B站</h1>
      <div className="bilibili-tabs" role="group" aria-label="B站内容">{tabs.map(([id, label, Icon]) => <button key={id} type="button" aria-pressed={tab === id} onClick={() => setTab(id)}><Icon size={15} aria-hidden="true"/>{label}</button>)}</div>
      <button className="icon-button" aria-label="刷新B站内容" title="刷新内容" disabled={state.loading} onClick={browser.refresh}>{state.loading ? <LoaderCircle size={17} className="spin"/> : <RefreshCw size={17}/>}</button>
    </div>
    <div className="bilibili-context">
      <span>{tab === 'ranking' ? '全站排行' : tab === 'dynamic' ? '关注投稿' : tab === 'my' ? '稍后再看' : tab === 'live' ? '直播浏览' : '热门视频'}{count > 0 && ` · ${count} 条`}</span>
      {tab === 'live' && <label>分区<select aria-label="直播分区" value={browser.area} onChange={event => browser.setArea(event.target.value)}><option value="1">娱乐</option><option value="2">游戏</option><option value="9">虚拟主播</option></select></label>}
      {tab === 'my' && <button className="text-button" onClick={onLibrary}><Bookmark size={14} aria-hidden="true"/>本机收藏</button>}
    </div>
    {tab === 'my' && profile?.loggedIn && <div className="bilibili-profile">
      {profile.avatar && <img src={profile.avatar} alt="" width={36} height={36} referrerPolicy="no-referrer"/>}
      <strong>{profile.name}</strong><span>Lv.{profile.level ?? 0}</span><a href={'https://space.bilibili.com/' + profile.id} target="_blank" rel="noopener noreferrer">个人空间<ExternalLink size={13}/></a>
    </div>}
    {(tab === 'my' || tab === 'dynamic') && profile && !profile.loggedIn && <div className="bilibili-login-prompt"><p>{tab === 'dynamic' ? '登录后查看我关注的 UP 主视频动态。' : '登录后查看稍后再看。'}</p><button className="button button-secondary" onClick={onLogin}>前往登录</button></div>}
    {state.error && <div className="inline-feedback" role="alert"><p>{state.error}</p><button className="text-button" onClick={onLogin}>登录设置</button><button className="text-button" disabled={state.loading} onClick={browser.refresh}>重试</button></div>}
    {notice && <p className="inline-feedback" role={noticeError ? 'alert' : 'status'}>{notice}</p>}
    {count > 0 ? tab === 'live'
      ? <CardCollection className="mixed-gallery" data-media="video" masonry={false} aria-label="直播列表">{state.page!.rooms!.map(room => <LiveCard key={room.id} room={room}/>)}</CardCollection>
      : <CardCollection className="mixed-gallery" data-media="video" masonry={false} aria-label="B站视频">{state.page!.items.map((item, index) => <ModernGalleryCard discovery key={item.id} item={item} saved={savedIDs.has(item.id)} pending={unavailable || pending.has(item.id)} onRead={onRead} onSave={onSave} status={tab === 'ranking' ? <span className="bilibili-rank">第 {index + 1} 名</span> : tab === 'dynamic' && item.viewCount ? <span className="bilibili-rank">{viewLabel(item.viewCount)}</span> : undefined}/>)}</CardCollection>
      : !(tab === 'my' && profile && !profile.loggedIn) && !state.error && <p className="bilibili-empty" role="status">{state.loading ? '正在获取内容…' : empty[tab]}</p>}
    {state.page?.hasMore && <div ref={sentinel} className="load-more"><button className="button button-secondary" onClick={browser.more} disabled={state.loading}>{state.loading ? <LoaderCircle size={15} className="spin"/> : <ListVideo size={15}/>}继续浏览</button></div>}
  </section>
}
