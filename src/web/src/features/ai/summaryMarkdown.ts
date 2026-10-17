import type { SummaryResult } from './summaryTypes'

export interface SummarySource { contentId: string; title?: string; url?: string }

function text(value: string): string {
  return value.replaceAll('&', '&amp;').replace(/[\\`*_{}[\]()<>#+.!|~-]/g, '\\$&')
}

function sourceURL(value: string | undefined): URL | null {
  if (!value) return null
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url : null
  } catch { return null }
}

function referenceURL(source: SummarySource, start: number): string | undefined {
  const url = sourceURL(source.url)
  if (!url) return undefined
  if (source.contentId.startsWith('youtube:') && url.hostname === 'www.youtube.com' && url.pathname === '/watch' &&
    url.searchParams.get('v') === source.contentId.slice('youtube:'.length)) {
    url.searchParams.set('t', `${Math.floor(start)}s`)
  } else if (source.contentId.startsWith('bilibili:') && url.hostname === 'www.bilibili.com' &&
    url.pathname.replace(/\/$/, '') === `/video/${source.contentId.slice('bilibili:'.length)}`) {
    // The subtitle reader currently reads the first part only.
    url.searchParams.set('p', '1')
    url.searchParams.set('t', String(Math.floor(start)))
  }
  return url.href
}

export function summaryMarkdown(result: SummaryResult, source: SummarySource, readerURL: string): string {
  if (!result.complete || !result.overview?.trim() || !result.points?.length) throw new Error('摘要尚未完成。')
  const reader = sourceURL(readerURL)
  if (!reader) throw new Error('阅读地址无效，请重新打开内容。')
  const citations = new Map<number, NonNullable<SummaryResult['citations']>[number]>()
  for (const citation of result.citations ?? []) {
    const timed = citation.start !== undefined || citation.end !== undefined
    if (!Number.isSafeInteger(citation.index) || citation.index < 0 || citations.has(citation.index) ||
      !citation.quote?.trim() || timed && (citation.start === undefined || citation.end === undefined ||
        !Number.isFinite(citation.start) || !Number.isFinite(citation.end) || citation.start < 0 || citation.end <= citation.start)) {
      throw new Error('摘要引用不完整，请重新读取。')
    }
    citations.set(citation.index, citation)
  }
  const url = sourceURL(source.url)
  const heading = source.title?.trim().replace(/\s+/g, ' ') || 'AI 摘要'
  const lines = [`# ${text(heading)}`, '', `内容标识：${text(source.contentId)}`, '']
  if (url) lines.push(`[原内容](<${url.href}>)`, '')
  lines.push(`[在 Tangentry 查看](<${reader.href}>)`, '', '## 概述', '', text(result.overview), '', '## 要点', '')
  const used = new Set<number>()
  for (const [index, point] of result.points.entries()) {
    if (!point.text?.trim() || !point.indices.length || new Set(point.indices).size !== point.indices.length ||
      point.indices.some(value => !citations.has(value))) throw new Error('摘要引用不完整，请重新读取。')
    lines.push(`${index + 1}. ${text(point.text).replaceAll('\n', '\n   ')}`, `   引用：${point.indices.map(value => `[${value + 1}]`).join(' ')}`, '')
    for (const value of point.indices) used.add(value)
  }
  lines.push('## 原文引用', '')
  for (const index of used) {
    const citation = citations.get(index)!
    const timed = citation.start !== undefined && citation.end !== undefined
    const location = timed ? ` · ${citation.start}–${citation.end} 秒` : ''
    const link = timed ? referenceURL(source, citation.start!) : url?.href
    lines.push(`### [${index + 1}]${location}`, '')
    if (link) lines.push(`[打开原内容](<${link}>)`, '')
    lines.push(...text(citation.quote).split('\n').map(line => `> ${line}`), '')
  }
  return lines.join('\n')
}
