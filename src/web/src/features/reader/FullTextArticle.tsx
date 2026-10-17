import { Download, RefreshCw, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { APIError, request } from '../../shared/api/client'
import type { ArticleDocument, ContentItem, ReadingNote, TextAnchor } from '../../shared/api/types'
import { ArticleReader, type ArticleReaderHandle } from './ArticleReader'
import { hashArticle, resolveTextAnchor } from '../../shared/text/textAnchors'
import { useSummary } from '../ai/useSummary'
import { SummaryControl } from '../ai/SummaryControl'
import { cardDescription } from '../../shared/ui/cardDescription'
import { articleSource, rememberArticleSource } from './articleSource'
import { ReaderActions } from '../../shared/reader/ReaderActions'
import { isLikelyMarkdown, markdownToPlainText } from './markdownContent'

export function FullTextArticle({ item, notes, focus, onQuote, onOpenNotes, onUnresolved, navigationTarget, onReturnedToReading }: {
  item: ContentItem; notes: ReadingNote[]; focus?: { id: string; token: number; allowReturn?: boolean } | null
  onQuote?: (text: string, anchor?: TextAnchor) => void; onOpenNotes?: (ids: string[]) => void; onUnresolved?: (ids: string[]) => void
  navigationTarget?: HTMLElement | null; onReturnedToReading?: () => void
}) {
  const [doc, setDoc] = useState<ArticleDocument | null>(null)
  const article = useRef<ArticleReaderHandle>(null)
  const preferredSource = useRef(articleSource(item.id))
  const [source, setSource] = useState<'feed' | 'original'>('feed')
  const [busy, setBusy] = useState<'read' | 'fetch' | null>('read')
  const [known, setKnown] = useState(false), [conflict, setConflict] = useState(false), [error, setError] = useState('')
  const [feedHash, setFeedHash] = useState({ text: '', hash: '' })
  const controller = useRef<AbortController | null>(null)
  const attempted = useRef(false)
  const sourceChoice = useRef(0)
  const manualChoice = useRef(0)
  const handledFocus = useRef<{ id: string; token: number } | null>(null)
  const [dismissedFocus, setDismissedFocus] = useState<number | null>(null)
  const requestedFocus = focus && focus.token !== dismissedFocus ? focus : null
  const chooseSource = (value: 'feed' | 'original') => {
    ++sourceChoice.current
    ++manualChoice.current
    handledFocus.current = focus ?? null
    setDismissedFocus(focus?.token ?? null)
    preferredSource.current = value
    rememberArticleSource(item.id, value)
    setSource(value)
  }
  const endpoint = '/articles/' + encodeURIComponent(item.id)
  const description = cardDescription(item)
  const feedBlocks = item.articleBlocks?.length ? item.articleBlocks : undefined
  const feedText = description || (feedBlocks ? feedBlocks.filter(block => block.kind !== 'image').map(block => block.text).join('\n').trim() : '') || '来源没有提供正文，可打开原内容查看。'
  const feedMarkdown = isLikelyMarkdown(feedText)
  const feedAnchorText = feedMarkdown ? markdownToPlainText(feedText) : feedText
  useEffect(() => {
    let alive = true
    void hashArticle(feedAnchorText).then(hash => { if (alive) setFeedHash({ text: feedAnchorText, hash }) }).catch(() => {})
    return () => { alive = false }
  }, [feedAnchorText])
  const read = async (initial = false) => {
    controller.current?.abort()
    const choiceAtStart = sourceChoice.current
    const next = new AbortController(); controller.current = next
    setBusy('read'); setError('')
    try {
      const value = await request<ArticleDocument>(endpoint, { signal: next.signal })
      if (next.signal.aborted) return
      setDoc(value); setKnown(true); setConflict(false)
      if (initial && sourceChoice.current === choiceAtStart && preferredSource.current !== 'feed') setSource('original')
    } catch (cause) {
      if (next.signal.aborted) return
      if (cause instanceof APIError && cause.code === 'article_not_found') { setDoc(null); setKnown(true); setConflict(false); setSource('feed') }
      else { setKnown(false); setError(cause instanceof Error ? cause.message : '本机全文未能读取。') }
    } finally { if (!next.signal.aborted) { setBusy(null); controller.current = null } }
  }
  useEffect(() => { void read(true); return () => controller.current?.abort() }, [endpoint])
  const fetchOriginal = async (automatic = false) => {
    if (!known || busy || conflict) return
    const choiceAtStart = sourceChoice.current
    const manualAtStart = manualChoice.current
    const next = new AbortController(); controller.current = next; setBusy('fetch'); setError('')
    try {
      const value = await request<ArticleDocument>(endpoint + '/fetch', { method: 'POST', signal: next.signal, body: JSON.stringify({ expectedRevision: doc?.revision ?? 0 }) })
      if (!next.signal.aborted) {
        setDoc(value)
        if (!automatic && manualChoice.current === manualAtStart) {
          preferredSource.current = 'original'
          rememberArticleSource(item.id, 'original')
        }
        if (sourceChoice.current === choiceAtStart && (!automatic || preferredSource.current !== 'feed')) {
          setSource('original')
        }
      }
    } catch (cause) {
      if (!next.signal.aborted) {
        setError(cause instanceof Error ? cause.message : '未能获取原站全文。')
        if (cause instanceof APIError && cause.code === 'article_conflict') setConflict(true)
      }
    } finally { if (!next.signal.aborted) { setBusy(null); controller.current = null } }
  }
  useEffect(() => {
    if (known && !doc && !busy && !conflict && !attempted.current) {
      attempted.current = true
      void fetchOriginal(true)
    }
  }, [known, doc, busy, conflict])
  const cancel = () => {
    controller.current?.abort(); controller.current = null; setBusy(null)
    // The response may have been lost after a successful write. Re-read before retrying.
    setKnown(false); setError('已停止等待；再次获取前，请先读取本机版本。')
  }
  const hash = feedHash.text === feedAnchorText ? feedHash.hash : ''
  const summaryText = source === 'original' && doc ? doc.text : description
  const summary = useSummary(item.id, `article:${source}`, undefined, undefined, summaryText)
  const missing = useMemo(() => notes.filter(note => note.textAnchor && !resolveTextAnchor(feedAnchorText, hash, note.textAnchor) && (!doc || !resolveTextAnchor(doc.text, doc.sourceHash, note.textAnchor))).map(note => note.id), [notes, feedAnchorText, hash, doc])
  useEffect(() => { if (hash && known) onUnresolved?.(missing) }, [missing, hash, known, onUnresolved])
  useEffect(() => {
    if (!requestedFocus || handledFocus.current?.id === requestedFocus.id && handledFocus.current.token === requestedFocus.token) return
    const anchor = notes.find(note => note.id === requestedFocus.id)?.textAnchor
    if (!anchor || !hash) return
    const feed = resolveTextAnchor(feedAnchorText, hash, anchor), original = doc && resolveTextAnchor(doc.text, doc.sourceHash, anchor)
    if (!feed && !original) return
    let next = source
    if (anchor.sourceHash === hash && feed) next = 'feed'
    else if (anchor.sourceHash === doc?.sourceHash && original) next = 'original'
    else if (source === 'feed' && !feed && original) next = 'original'
    else if (source === 'original' && !original && feed) next = 'feed'
    handledFocus.current = requestedFocus
    ++sourceChoice.current
    setSource(next)
  }, [requestedFocus, doc, hash, feedAnchorText, notes, source])
  return <section className="fulltext-reader" aria-label="文章正文来源">
      <div className="fulltext-toolbar">
      <div className="fulltext-sources" role="group" aria-label="选择正文来源">
        <button className="button button-secondary" aria-pressed={source === 'feed'} onClick={() => chooseSource('feed')}>订阅文字</button>
        {doc && <button className="button button-secondary" aria-pressed={source === 'original'} onClick={() => chooseSource('original')}>原站全文</button>}
      </div>
      {known && !conflict && <button className="button button-secondary" disabled={Boolean(busy)} onClick={() => void fetchOriginal()}>{doc ? <RefreshCw size={14} aria-hidden="true"/> : <Download size={14} aria-hidden="true"/>}{doc ? '更新原站全文' : '获取原站全文'}</button>}
      {(!known || conflict) && !busy && <button className="button button-secondary" onClick={() => void read()}><RefreshCw size={14} aria-hidden="true"/>读取本机版本</button>}
      {busy === 'fetch' && <button className="button button-secondary" onClick={cancel}><X size={14} aria-hidden="true"/>取消获取</button>}
      {summaryText && <ReaderActions><SummaryControl summary={summary} source={{ contentId: item.id, title: item.title, url: source === 'original' && doc ? doc.sourceUrl : item.url }} onLocateQuote={quote => article.current ? article.current.locateQuote(quote) : '原文还未就绪，请稍后重试。'} /></ReaderActions>}
    </div>
    {busy && <p className="fulltext-status" role="status">{busy === 'read' ? '正在读取本机全文…' : '正在获取原站正文…'}</p>}
    {error && <p className="fulltext-status" role="alert">{error}</p>}
    <ArticleReader ref={article} key={source} contentId={summaryText ? item.id : ''} title={source === 'original' && doc ? '原站全文' : '订阅源正文'} text={source === 'original' && doc ? doc.text : feedText} blocks={source === 'original' && doc ? doc.blocks : feedMarkdown ? undefined : feedBlocks} feed={source === 'feed'} toolbarOwnsSummary notes={notes} focus={requestedFocus} sourceURL={source === 'original' && doc ? doc.sourceUrl : item.url} onQuote={source === 'original' && doc || description ? onQuote : undefined} onOpenNotes={onOpenNotes} navigationTarget={navigationTarget} onReturnedToReading={onReturnedToReading}/>
  </section>
}
