import type { SubtitleAssistance } from '../../shared/ui/subtitleAssistance'
import { ArrowLeft, ExternalLink, LoaderCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem, Feed, SubtitleTrack, SubtitleReference } from '../../shared/api/types'
import { formatDate, formatDuration } from '../../shared/ui/ContentCard'
import { SubtitleReader } from './SubtitleReader'
import { BilingualTitle } from '../ai/TextTranslation'
import { ReaderActionsSlot } from '../../shared/reader/ReaderActions'
import { ReadingNotebook } from '../../shared/reader/ReadingNotebook'
import { ReaderDescription } from '../reader/ReaderDescription'
import '../reader/reader.css'
import '../reader/watch.css'

export function SubtitlePage({ query, initialItem, onReturn, notes, onNoteAt, player, position, onSeek, onTrackChange, assistance }: {
  query: string; initialItem?: ContentItem; onReturn: () => void; notes?: ReactNode; onNoteAt?: (position: number, reference?: SubtitleReference) => void
  assistance?: SubtitleAssistance
  player?: ReactNode; position?: number | null; onSeek?: (position: number) => void; onTrackChange?: (track: SubtitleTrack | null) => void
}) {
  const [item, setItem] = useState<ContentItem | null>(initialItem ?? null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const valid = /^BV[0-9A-Za-z]{10}$/.test(query)

  useEffect(() => {
    if (!valid) return
    if (initialItem?.platformId === query) { setItem(initialItem); setError(''); return }
    const controller = new AbortController()
    setError('')
    void request<Feed>(`/discovery/bilibili?q=${encodeURIComponent(query)}&page=1`, { signal: controller.signal }).then(feed => {
      const video = feed.items.find(candidate => candidate.platformId === query)
      if (!video) throw new Error('没有找到这段视频，请返回列表重试。')
      if (!controller.signal.aborted) setItem(video)
    }).catch(cause => {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '视频信息暂时无法获取。')
    })
    return () => controller.abort()
  }, [query, initialItem, valid, attempt])

  return <section className="subtitle-page video-workspace">
    <div className="reading-nav"><button className="button button-secondary" onClick={onReturn}><ArrowLeft size={16} />返回列表</button><span>视频阅读 · 第一分 P</span><ReaderActionsSlot/><a className="button button-secondary" href={`https://www.bilibili.com/video/${query}/`} target="_blank" rel="noopener noreferrer">打开原视频 <ExternalLink size={15} /></a></div>
    {!valid ? <><h1 className="reading-title">视频阅读</h1><p role="alert" className="subtitle-error">阅读地址缺少正确的视频 BV 号，请返回列表选择视频。</p></> : <>
      <ReadingNotebook enabled={Boolean(notes)}><div className="reader-primary">
        <div className="watch-details">{item ? <BilingualTitle heading contentId={item.id} text={item.title}/> : <h1 className="reading-video-title">{query}</h1>}
        {item && <p className="reading-meta">{item.author || '未知作者'} · {formatDate(item.publishedAt)}{item.durationSeconds > 0 && ` · ${formatDuration(item.durationSeconds)}`}</p>}
        {item?.description && <ReaderDescription key={item.id} item={item} onSeek={onSeek} className="watch-description"/>}</div>{player}
        {error ? <div className="subtitle-error" role="alert"><p>{error}</p><button className="button button-secondary" onClick={() => setAttempt(value => value + 1)}>重新获取视频信息</button></div> : !item && <p className="subtitle-note" role="status"><LoaderCircle size={16} className="spin" />正在获取视频信息…</p>}<SubtitleReader key={query} item={item ?? { platformId: query, url: `https://www.bilibili.com/video/${query}/` }} onNoteAt={onNoteAt} position={position} onSeek={onSeek} onTrackChange={onTrackChange} assistance={assistance} /></div><aside className="reading-notes">{notes}</aside></ReadingNotebook>
    </>}
  </section>
}
