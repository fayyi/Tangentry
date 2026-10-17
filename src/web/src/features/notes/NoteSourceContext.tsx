import { ArrowUpRight, LoaderCircle, RefreshCw, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { APIError, request } from '../../shared/api/client'
import type { ArticleDocument, ContentItem, ReadingNote, SavedSubtitle } from '../../shared/api/types'
import { hashArticle, resolveTextAnchor } from '../../shared/text/textAnchors'
import { SubtitleReferences } from './SubtitleReferences'
import { NoteFrame } from './NoteFrame'
import { subtitlePriority } from '../subtitles/useSubtitles'
import { formatPosition } from './NotesPanel'

interface SourceSnapshot {
  text?: Pick<ArticleDocument, 'text' | 'sourceHash'>
  item?: ContentItem
  tracks: SavedSubtitle[]
  errors: string[]
}

async function optionalLocal<T>(path: string, signal: AbortSignal): Promise<T | undefined> {
  try { return await request<T>(path, { signal }) }
  catch (error) {
    if (error instanceof APIError && error.status === 404) return undefined
    throw error
  }
}

export function NoteSourceContext({ note, title, item, onClose, onRead }: {
  note: ReadingNote | null; title: string; item?: ContentItem
  onClose: () => void; onRead: (note: ReadingNote) => void
}) {
  const heading = useRef<HTMLHeadingElement>(null)
  const cache = useRef(new Map<string, SourceSnapshot>())
  const [snapshot, setSnapshot] = useState<SourceSnapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [revision, setRevision] = useState(0)
  const id = note?.contentId ?? ''
  const timed = Boolean(note && (note.positionSeconds !== null || ['video', 'audio'].includes(item?.kind ?? note.source?.kind ?? '')))
  const key = `${id}:${timed}`
  useEffect(() => {
    if (note) heading.current?.focus({ preventScroll: true })
  }, [note?.id])
  useEffect(() => {
    setSnapshot(null)
    if (!id) { setLoading(false); return }
    const existing = cache.current.get(key)
    if (existing) { setSnapshot(existing); setLoading(false); return }
    const controller = new AbortController()
    setLoading(true)
    const reads = [
      id.startsWith('rss:') ? optionalLocal<ArticleDocument>(`/articles/${encodeURIComponent(id)}`, controller.signal).then(article => {
        if (article && (article.contentId !== id || typeof article.text !== 'string' || typeof article.sourceHash !== 'string')) throw new Error('本机正文与当前来源不匹配。')
        return article
      }) : Promise.resolve(undefined),
      id.startsWith('rss:') && !item ? optionalLocal<ContentItem>(`/subscription-inbox/${encodeURIComponent(id)}`, controller.signal) : Promise.resolve(undefined),
      timed ? request<{ tracks: SavedSubtitle[] }>(`/saved-subtitles?contentId=${encodeURIComponent(id)}`, { signal: controller.signal }) : Promise.resolve(undefined),
      id.startsWith('xiaohongshu:') ? optionalLocal<{ item: ContentItem | null; complete: boolean }>(`/xiaohongshu/details/${encodeURIComponent(id)}`, controller.signal).then(async detail => {
        const local = detail?.item
        if (!local) return undefined
        if (local.id !== id || local.platform !== 'xiaohongshu' || local.platformId !== id.slice(12) || typeof local.description !== 'string') throw new Error('本机笔记与当前来源不匹配。')
        return { item: local, text: detail?.complete === true ? { text: local.description, sourceHash: await hashArticle(local.description) } : undefined }
      }) : Promise.resolve(undefined),
    ] as const
    void Promise.allSettled(reads).then(([article, localItem, subtitles, detail]) => {
      if (controller.signal.aborted) return
      const value: SourceSnapshot = {
        text: detail.status === 'fulfilled' && detail.value?.text || (article.status === 'fulfilled' ? article.value : undefined),
        item: detail.status === 'fulfilled' && detail.value?.item || (localItem.status === 'fulfilled' ? localItem.value : undefined),
        tracks: subtitles.status === 'fulfilled' ? subtitles.value?.tracks ?? [] : [],
        errors: [article, localItem, subtitles, detail].flatMap(result => result.status === 'rejected' ? [result.reason instanceof Error ? result.reason.message : '本机来源暂时无法读取。'] : []),
      }
      // Failed reads are retried on reopening; confirmed local snapshots can be reused.
      if (!value.errors.length) {
        if (cache.current.size >= 8) cache.current.delete(cache.current.keys().next().value!)
        cache.current.set(key, value)
      }
      setSnapshot(value)
      setLoading(false)
    })
    return () => controller.abort()
  }, [id, key, timed, item?.id, revision])
  if (!note) return null
  const text = snapshot?.text
  const quote = note.textAnchor?.quote ?? note.pdfAnchor?.quote ?? ''
  const resolved = text && note.textAnchor ? resolveTextAnchor(text.text, text.sourceHash, note.textAnchor) : null
  const track = snapshot?.tracks.slice().sort((a, b) => subtitlePriority(a.track) - subtitlePriority(b.track))[0]?.track
  const position = note.positionSeconds
  const cues = track?.cues ?? []
  let cueIndex = -1
  if (position !== null) {
    for (let i = 0; i < cues.length; i++) {
      if (cues[i].start > position) break
      cueIndex = i
    }
  }
  const nearby = cueIndex >= 0 ? cues.slice(Math.max(0, cueIndex - 2), cueIndex + 3) : []
  const fallback = snapshot?.item ?? item
  const closeOnEscape = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.stopPropagation(); onClose() } }
  return <aside className="note-source-context" aria-label="笔记来源上下文" onKeyDown={closeOnEscape}>
    <div className="note-context-heading"><h2 ref={heading} tabIndex={-1}>来源上下文</h2><button className="icon-button" disabled={loading} title="重新读取本机来源" aria-label="重新读取本机来源" onClick={() => { cache.current.delete(key); setRevision(value => value + 1) }}><RefreshCw size={16}/></button><button className="icon-button" title="关闭来源上下文" aria-label="关闭来源上下文" onClick={onClose}><X size={18}/></button></div>
    <h3 className="note-context-title">{title}</h3>
    {(fallback?.author || note.source?.author) && <p className="note-context-meta">{fallback?.author || note.source?.author}</p>}
    <div className="note-context-body">
      {loading && <p className="note-context-meta" role="status"><LoaderCircle size={15} className="spin"/>正在读取本机来源…</p>}
      {snapshot?.errors.length ? <p role="alert" className="notes-error">{[...new Set(snapshot.errors)].join(' ')}</p> : null}
      {note.pdfAnchor && <p className="note-context-meta">PDF · 第 {note.pdfAnchor.page} 页</p>}
      <SubtitleReferences references={note.subtitleReferences} contentId={note.contentId}/>
      <NoteFrame note={note}/>
      {quote && <section aria-label="原文摘录">
        <p className="note-context-meta">{resolved ? '本机全文 · 摘录位置已匹配' : '笔记保存的摘录'}</p>
        <div className="note-context-passage">
          <span>{resolved && text ? Array.from(text.text.slice(0, resolved.start)).slice(-400).join('') : note.textAnchor?.prefix}</span>
          <mark>{quote}</mark>
          <span>{resolved && text ? Array.from(text.text.slice(resolved.end)).slice(0, 400).join('') : note.textAnchor?.suffix}</span>
        </div>
        {!loading && text && note.textAnchor && !resolved && <p className="note-context-meta">缓存全文中未找到唯一匹配，保留原摘录。</p>}
      </section>}
      {position !== null && <section aria-label="附近字幕"><p className="note-context-meta">关联时间 {formatPosition(position)}{track && ` · ${track.label}`}</p>
        {nearby.length > 0 ? <ol className="note-context-cues">{nearby.map((cue, index) => <li key={`${cue.start}:${index}`} data-current={cue === cues[cueIndex] && position < cue.end ? 'true' : undefined}><time>{formatPosition(cue.start)}</time><span>{cue.text}</span></li>)}</ol>
          : !loading && <p className="note-context-meta">{track ? '此时间附近没有已保存字幕。' : '本机还没有保存此来源的字幕。'}</p>}
      </section>}
      {!quote && position === null && !loading && <section aria-label="本机正文预览"><p className="note-context-meta">本机正文预览</p><p className="note-context-passage">{Array.from(text?.text || fallback?.description || fallback?.previewText || '').slice(0, 1400).join('') || '本机没有保存正文片段，可进入阅读页查看来源。'}</p></section>}
    </div>
    <button type="button" className="button button-secondary note-context-read" onClick={() => onRead(note)}>回到来源阅读<ArrowUpRight size={15}/></button>
  </aside>
}
