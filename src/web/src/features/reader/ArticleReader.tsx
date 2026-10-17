import { ArrowLeft, BookOpen, StickyNote, X } from 'lucide-react'
import { Fragment, useEffect, useImperativeHandle, useMemo, useRef, useState, type ReactNode, type Ref } from 'react'
import { createPortal } from 'react-dom'
import type { ArticleBlock, ReadingNote, TextAnchor } from '../../shared/api/types'
import { anchorSelection, hashArticle, highlightSegments, resolveTextAnchor } from '../../shared/text/textAnchors'
import { TextTranslationControl, useTextTranslation } from '../ai/TextTranslation'
import { articleTranslationParts } from './articleTranslation'
import { useSummary } from '../ai/useSummary'
import { SummaryControl } from '../ai/SummaryControl'
import { useArticlePosition } from './useArticlePosition'
import { ArticleReflowGuard } from './ArticleReflowGuard'
import { originalRunAt } from './articleViewport'
import { SourceText } from './SourceText'
import { ArticleImage } from './ArticleImage'
import { ReaderActions } from '../../shared/reader/ReaderActions'
import { shouldRenderInlineSummary } from '../ai/summaryPresentation'
import MarkdownRenderer from '../notes/MarkdownRenderer'
import { isLikelyMarkdown, markdownToPlainText } from './markdownContent'
import { proxiedImageURL } from '../../shared/media/sourceImage'

export interface ArticleReaderHandle { locateQuote: (quote: string) => string | null }

export function ArticleReader({ ref: externalRef, contentId = '', text, blocks, feed, notes, focus, onQuote, onOpenNotes, onUnresolved, navigationTarget, onReturnedToReading, collapsible = false, title = '订阅源正文', sourceURL, sourceTitle, summarySource = contentId.startsWith('xiaohongshu:') ? 'article:social' : undefined, toolbarOwnsSummary = false }: {
  ref?: Ref<ArticleReaderHandle>
  contentId?: string
  summarySource?: string
  toolbarOwnsSummary?: boolean
  sourceURL?: string
  sourceTitle?: string
  blocks?: ArticleBlock[]
  text: string; feed: boolean; notes: ReadingNote[]; focus?: { id: string; token: number; allowReturn?: boolean } | null
  onQuote?: (text: string, anchor?: TextAnchor) => void; onOpenNotes?: (ids: string[]) => void; onUnresolved?: (ids: string[]) => void
  title?: string
  collapsible?: boolean
  navigationTarget?: HTMLElement | null; onReturnedToReading?: () => void
}) {
  const root = useRef<HTMLDivElement>(null)
  const handledFocus = useRef<{ id: string; token: number } | null>(null)
  const [hashed, setHashed] = useState({ text: '', hash: '' })
  const [selection, setSelection] = useState<TextAnchor | null>(null)
  const initialFocus = useRef(focus?.token)
  const [returnPoint, setReturnPoint] = useState<{ identity: string; offset: number; top: number } | null>(null)
  const [citation, setCitation] = useState<{ text: string; start: number; end: number } | null>(null)
  const [textExpanded, setTextExpanded] = useState(false)
  const [textOverflow, setTextOverflow] = useState(false)
  const markdown = !blocks && isLikelyMarkdown(text)
  const annotationText = markdown ? markdownToPlainText(text) : text
  const citationRange = citation?.text === annotationText ? citation : null
  const sourceHash = hashed.text === annotationText ? hashed.hash : ''
  const identity = contentId + ':' + sourceHash
  const returnAvailable = returnPoint?.identity === identity
  const readingPosition = useArticlePosition(root, contentId, sourceHash, annotationText.length, focus?.token)
  const paragraphs = useMemo(() => {
    if (blocks) return blocks.map(block => block.kind === 'code' || block.kind === 'image' || block.kind === 'emoji' ? null : block.text ?? '')
    return annotationText.split(/(\n\n+)/).filter((_, index) => index % 2 === 0)
  }, [blocks, annotationText])
  const blockOffsets = useMemo(() => {
    if (!blocks) return []
    let cursor = 0
    return blocks.map(block => {
      if (block.kind === 'image' || block.kind === 'emoji') return { start: cursor, end: cursor }
      const value = block.text ?? ''
      const found = annotationText.indexOf(value, cursor)
      const start = found < 0 ? cursor : found
      const end = Math.min(annotationText.length, start + value.length)
      cursor = end
      return { start, end }
    })
  }, [blocks, annotationText])
  const { pieces, ranges } = useMemo(() => articleTranslationParts(paragraphs), [paragraphs])
  const translation = useTextTranslation(contentId, pieces, 'article')
  const summary = useSummary(contentId, summarySource ?? '', undefined, undefined, contentId && summarySource ? text : undefined)
  const translatedParagraphs = ranges.map(({ start, end }) => translation.inspection?.result.texts.slice(start, end).join(''))
  useEffect(() => { setTextExpanded(false) }, [contentId, text])
  useEffect(() => {
    if (!collapsible || !root.current) return
    const node = root.current
    const measure = () => setTextOverflow(node.scrollHeight > Math.min(window.innerHeight * .32, 280) + 1)
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    window.addEventListener('resize', measure)
    measure()
    return () => { observer.disconnect(); window.removeEventListener('resize', measure) }
  }, [collapsible, text, translation.show, translation.inspection?.result.texts])
  useEffect(() => {
    let current = true
    setSelection(null)
    setReturnPoint(null)
    setCitation(null)
    void hashArticle(annotationText).then(hash => { if (current) setHashed({ text: annotationText, hash }) }).catch(() => { if (current) setHashed({ text: annotationText, hash: '' }) })
    return () => { current = false }
  }, [annotationText])
  const resolved = useMemo(() => notes.filter(note => note.textAnchor).map(note => ({ id: note.id, range: resolveTextAnchor(annotationText, sourceHash, note.textAnchor!) })), [notes, annotationText, sourceHash])
  const segments = useMemo(() => highlightSegments(annotationText, resolved.flatMap(value => value.range ? [{ id: value.id, ...value.range }] : []), citationRange ? [citationRange.start, citationRange.end] : []), [annotationText, resolved, citationRange])
  const locateQuote = (quote: string): string | null => {
    if (!sourceHash || !root.current) return '原文还未就绪，请稍后重试。'
    if (!quote.trim()) return '摘要引用没有可定位的原文。'
    const candidate = annotationText.indexOf(quote) >= 0 ? quote : markdown ? markdownToPlainText(quote) : quote
    const start = annotationText.indexOf(candidate)
    if (start < 0) return '当前原文中未找到这段引用，请核对摘要与正文来源。'
    if (annotationText.indexOf(candidate, start + 1) >= 0) return '原文中有多处相同引用，暂时无法确定位置。'
    const point = readingPosition.capture()
    if (point) setReturnPoint(current => current?.identity === identity ? current : { identity, ...point })
    readingPosition.pause()
    setTextExpanded(true)
    setCitation({ text: annotationText, start, end: start + candidate.length })
    return null
  }
  useImperativeHandle(externalRef, () => ({ locateQuote }))
  useEffect(() => {
    if (!citationRange || !sourceHash) return
    readingPosition.pause()
    const node = root.current?.querySelector<HTMLElement>('[data-summary-citation="true"]')
    if (node) {
      node.focus({ preventScroll: true })
      node.scrollIntoView({ block: 'center', behavior: 'instant' })
      return
    }
    // Markdown is rendered by react-markdown rather than the offset-aware text
    // renderer. Locate the same visible quote as a fallback for scrolling.
    const walker = root.current && document.createTreeWalker(root.current, NodeFilter.SHOW_TEXT)
    while (walker) {
      const current = walker.nextNode()
      if (!current) break
      if (current.textContent?.includes(citationRange.text.slice(citationRange.start, citationRange.end))) {
        const parent = current.parentElement
        parent?.scrollIntoView({ block: 'center', behavior: 'instant' })
        break
      }
    }
  }, [citationRange, sourceHash])
  useEffect(() => { if (sourceHash) onUnresolved?.(resolved.filter(value => !value.range).map(value => value.id)) }, [sourceHash, resolved, onUnresolved])
  useEffect(() => {
    if (!focus || !sourceHash || handledFocus.current?.id === focus.id && handledFocus.current.token === focus.token) return
    if (collapsible && !textExpanded) { setTextExpanded(true); return }
    let node = root.current?.querySelector<HTMLElement>(`[data-note-ids~="${focus.id}"]`)
    if (!node) {
      const quote = notes.find(note => note.id === focus.id)?.textAnchor?.quote
      if (quote && root.current) {
        const walker = document.createTreeWalker(root.current, NodeFilter.SHOW_TEXT)
        while (walker.nextNode()) {
          if (walker.currentNode.textContent?.includes(quote)) {
            node = walker.currentNode.parentElement
            break
          }
        }
      }
    }
    if (!node) return
    handledFocus.current = focus
    if (focus.allowReturn !== false && focus.token !== initialFocus.current) {
      const point = readingPosition.capture()
      if (point) setReturnPoint(current => current?.identity === identity ? current : { identity, ...point })
    }
    readingPosition.pause()
    node.focus({ preventScroll: true })
    node.scrollIntoView({ block: 'center', behavior: 'instant' })
  }, [focus, sourceHash, segments, collapsible, textExpanded])
  const returnToReading = () => {
    if (!returnPoint || !returnAvailable || !root.current) return
    const node = originalRunAt(root.current, returnPoint.offset)
    if (!node) return
    readingPosition.pause()
    root.current.focus({ preventScroll: true })
    window.scrollBy({ top: node.getBoundingClientRect().top - returnPoint.top, behavior: 'instant' })
    setReturnPoint(null)
    setCitation(null)
    onReturnedToReading?.()
  }
  const returnControls = <><button type="button" onClick={returnToReading}><ArrowLeft size={15} aria-hidden="true"/>返回阅读位置</button><button type="button" aria-label="关闭阅读返回" title="关闭阅读返回" onClick={() => { setReturnPoint(null); setCitation(null); root.current?.focus({ preventScroll: true }) }}><X size={15} aria-hidden="true"/></button></>
  const capture = () => { if (root.current) setSelection(anchorSelection(root.current, annotationText, sourceHash)) }
  useEffect(() => {
    // selectionchange also covers touch selection handles and keyboard selection.
    document.addEventListener('selectionchange', capture)
    return () => document.removeEventListener('selectionchange', capture)
  }, [annotationText, sourceHash])
  const open = (ids: string[]) => { if (!window.getSelection()?.isCollapsed) return; onOpenNotes?.(ids) }
  // Separators between structured blocks are whitespace; they keep their offsets
  // for selection mapping but never become a highlight of their own.
  const renderSegments = (start: number, end: number, recognize = true, separator = false) => segments.filter(segment => segment.start < end && segment.start + segment.text.length > start).map(segment => {
    const value = segment.text.slice(Math.max(0, start - segment.start), Math.min(segment.text.length, end - segment.start))
    const originalStart = Math.max(start, segment.start), originalEnd = Math.min(end, segment.start + segment.text.length)
    const cited = Boolean(citationRange && originalStart >= citationRange.start && originalEnd <= citationRange.end)
    if (separator && !value.trim()) return <span key={segment.start} data-article-start={originalStart} data-article-end={originalEnd}>{value}</span>
    const displayed = recognize ? <SourceText text={value} social={contentId.startsWith('xiaohongshu:')} interactive={!segment.ids.length && !cited}/> : value
    return segment.ids.length ? <mark key={segment.start} className={`article-highlight${cited ? ' article-summary-highlight' : ''}`} role="button" tabIndex={0} data-summary-citation={cited || undefined} data-article-start={originalStart} data-article-end={originalEnd} data-note-ids={segment.ids.join(' ')} data-current={Boolean(focus && segment.ids.includes(focus.id))} aria-label={`查看对应批注：${value.slice(0, 40)}${segment.ids.length > 1 ? `，${segment.ids.length} 条笔记` : ''}`} onClick={() => open(segment.ids)} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpenNotes?.(segment.ids) } }}>{displayed}</mark> : cited ? <mark key={segment.start} className="article-summary-highlight" tabIndex={-1} data-summary-citation="true" data-article-start={originalStart} data-article-end={originalEnd}>{displayed}</mark> : <span key={segment.start} data-article-start={originalStart} data-article-end={originalEnd}>{displayed}</span>
  })
  let offset = 0
  const renderBlockText = (block: ArticleBlock, start: number, end: number, recognize = true) => {
    const images = (block.inlineImages ?? []).filter(image => image.offset >= 0 && image.offset <= block.text.length).sort((a, b) => a.offset - b.offset)
    if (!images.length) return renderSegments(start, end, recognize)
    const parts: ReactNode[] = []
    let cursor = 0
    images.forEach((image, index) => {
      if (image.offset > cursor) parts.push(<Fragment key={`text-${index}`}>{renderSegments(start + cursor, start + image.offset, recognize)}</Fragment>)
      parts.push(<img key={`image-${index}`} className="article-inline-emoji" src={proxiedImageURL(image.url, sourceURL)} alt={image.alt ?? ''} width={image.width || undefined} height={image.height || undefined} loading="lazy" decoding="async" referrerPolicy="no-referrer" />)
      cursor = image.offset
    })
    if (cursor < block.text.length) parts.push(<Fragment key="text-end">{renderSegments(start + cursor, end, recognize)}</Fragment>)
    return parts
  }
  return <article className="article-reader" onMouseUp={event => { event.stopPropagation(); capture() }} onKeyUp={event => { event.stopPropagation(); capture() }}>
    {title && <h2>{title}</h2>}
    {contentId && summarySource && text.trim() && shouldRenderInlineSummary(feed, toolbarOwnsSummary) && <ReaderActions><SummaryControl summary={summary} source={{contentId,title:sourceTitle,url:sourceURL}} onLocateQuote={locateQuote}/></ReaderActions>}
    <div className="article-reading-tools">{contentId && pieces.length > 0 && <TextTranslationControl translation={translation} label="翻译文章"/>}{readingPosition.available && <button type="button" className="text-button article-resume" onClick={() => {
      if (collapsible && !textExpanded) { setTextExpanded(true); requestAnimationFrame(readingPosition.restore) }
      else readingPosition.restore()
    }} title="回到上次阅读的原文段落"><BookOpen size={14} aria-hidden="true"/>继续阅读</button>}</div>
    <ArticleReflowGuard root={root} contentId={contentId} text={annotationText} layout={translation.show ? translation.inspection?.result.texts : undefined} focusToken={focus?.token} beforeReflow={readingPosition.pause}>
      <div ref={root} tabIndex={-1} onFocusCapture={() => { if (collapsible) setTextExpanded(true) }} className={`article-text${blocks ? ' article-structured' : ''}${markdown ? ' article-markdown' : ''}${collapsible && !textExpanded ? ' article-text-collapsed' : ''}`} data-anchor-ready={Boolean(sourceHash)}>{markdown ? <MarkdownRenderer imageMode="inline" imageReferrer={sourceURL}>{text}</MarkdownRenderer> : blocks ? blocks.map((block, index) => {
      const text = block.text ?? ''
      const { start, end } = blockOffsets[index] ?? { start: offset, end: offset + text.length }
      if (block.kind === 'image' || block.kind === 'emoji') return <ArticleImage key={index} block={block} referrer={sourceURL}/>
      const separator = renderSegments(offset, start, false, true)
      offset = end
      const Tag = block.kind === 'heading' ? (block.level === 4 ? 'h4' : block.level === 3 ? 'h3' : 'h2') : block.kind === 'code' ? 'pre' : block.kind === 'quote' ? 'blockquote' : block.kind === 'list' ? (block.ordered ? 'ol' : 'ul') : block.kind === 'details' || block.kind === 'spoiler' ? 'details' : block.kind === 'onebox' ? 'p' : 'p'
      if (block.kind === 'details' || block.kind === 'spoiler') return <Fragment key={index}>{separator}<details className={`article-block article-block-${block.kind}`}><summary>{block.summary}</summary><p>{renderBlockText(block, start, end)}</p></details></Fragment>
      if (block.kind === 'onebox') return <Fragment key={index}>{separator}<p className="article-block article-block-onebox"><a href={block.url} target="_blank" rel="noreferrer">{renderBlockText(block, start, end)}</a></p></Fragment>
      if (block.kind === 'list') return <Fragment key={index}>{separator}<Tag className={`article-block article-block-list article-list-depth-${block.level ?? 0}`}><li>{renderBlockText(block, start, end)}</li></Tag></Fragment>
      if (block.kind === 'table') return <Fragment key={index}>{separator}<p className="article-block article-block-table">{renderBlockText(block, start, end)}</p></Fragment>
      return <Fragment key={index}>{separator}<Tag className={`article-block article-block-${block.kind}`}>{renderBlockText(block, start, end, block.kind !== 'code')}</Tag>{translation.show && translatedParagraphs[index] && <p data-translation="true" className="article-translated" lang="zh-Hans">{translatedParagraphs[index]}</p>}</Fragment>
    }).concat(renderSegments(offset, annotationText.length, false, true)) : annotationText.split(/(\n\n+)/).map((part, index) => {
      const start = offset; offset += part.length
      return <Fragment key={index}>{renderSegments(start, offset)}{index % 2 === 0 && translation.show && translatedParagraphs[index / 2] && <p data-translation="true" className="article-translated" lang="zh-Hans">{translatedParagraphs[index / 2]}</p>}</Fragment>
    })}</div>
    </ArticleReflowGuard>
    {collapsible && textOverflow && <button type="button" className="text-button article-expand" aria-expanded={textExpanded} onClick={() => { readingPosition.pause(); setTextExpanded(value => !value) }}>{textExpanded ? '收起正文' : '展开全文'}</button>}
    {selection && onQuote && <div className="article-selection"><button className="button button-secondary" onMouseDown={event => event.preventDefault()} onClick={() => { onQuote(selection.quote, selection); setSelection(null); window.getSelection()?.removeAllRanges() }}><StickyNote size={15} aria-hidden="true"/>引用选中文字到批注</button><span>{Array.from(selection.quote).length} 字 · 保存笔记后保留划线</span></div>}
    {feed && <p className="field-hint">显示订阅源提供的文字；摘要源可能不包含全文。</p>}
    {returnAvailable && <><div className="article-jump-return" data-mobile-target={Boolean(navigationTarget)} role="group" aria-label="返回正文阅读">{returnControls}</div>{navigationTarget && createPortal(<div className="article-return-mobile" role="group" aria-label="返回正文阅读">{returnControls}</div>, navigationTarget)}</>}
  </article>
}
