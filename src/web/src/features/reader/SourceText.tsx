import LinkifyIt from 'linkify-it'
import type { ReactNode } from 'react'
import { useMemo } from 'react'

const links = new LinkifyIt().set({ fuzzyEmail: false })
type Token = { start: number; end: number; href?: string; tag?: string }

function recognizeText(text: string, social: boolean, interactive: boolean) {
  const tokens: Token[] = (links.match(text) ?? []).flatMap(match => {
    try {
      // The URL parser accepts CJK sentence punctuation as part of a path.
      const punctuation = match.raw.match(/[。！？；，、]+$/u)?.[0] ?? ''
      const url = new URL(punctuation ? match.url.slice(0, -punctuation.length) : match.url)
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return []
      return [{ start: match.index, end: match.lastIndex - punctuation.length, href: url.href }]
    } catch { return [] }
  })
  {
    // XHS uses adjacent #topic[话题]# markers; keep the exact source label.
    for (const match of text.matchAll(/#[\p{L}\p{N}_][\p{L}\p{N}_-]*(?:\[话题\]#)?/gu)) {
      const start = match.index, end = start + match[0].length
      if (tokens.some(token => start < token.end && end > token.start)) continue
      if (start > 0 && /[\p{L}\p{N}_/]/u.test(text[start - 1])) continue
      const tag = match[0].slice(1).replace(/\[话题\]#$/, '')
      tokens.push({ start, end, tag, href: social ? 'https://www.xiaohongshu.com/search_result?keyword=' + encodeURIComponent(tag) : undefined })
    }
  }
  const nodes: ReactNode[] = []
  let start = 0
  for (const token of tokens.sort((a, b) => a.start - b.start)) {
    if (token.start > start) nodes.push(text.slice(start, token.start))
    const label = text.slice(token.start, token.end)
    const topicMarker = Boolean(social && token.tag && label.endsWith('[话题]#'))
    const content = topicMarker ? <>{'#' + token.tag}<span hidden aria-hidden="true">[话题]#</span></> : label
    nodes.push(token.href && interactive ? <a key={token.start} className={token.tag ? 'source-topic' : 'source-link'} aria-label={topicMarker ? '#' + token.tag : undefined} href={token.href} target="_blank" rel="noopener noreferrer" title={token.tag ? `在小红书搜索：${token.tag}` : token.href}>{content}</a> : <span key={token.start}>{content}</span>)
    start = token.end
  }
  if (start < text.length) nodes.push(text.slice(start))
  return <>{nodes}</>
}

export function SourceText({ text, social = false, interactive = true }: { text: string; social?: boolean; interactive?: boolean }) {
  return useMemo(() => recognizeText(text, social, interactive), [text, social, interactive])
}
