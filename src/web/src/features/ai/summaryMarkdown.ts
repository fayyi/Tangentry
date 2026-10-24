import type { SummaryEvidence, SummaryResult, SummarySection, SummaryUnit } from './summaryTypes'

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
  if (result.complete && result.document?.sections?.length) return summaryDocumentMarkdown(result, source, readerURL)
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
  if (result.outline?.length) {
    lines.push('## 大纲', '')
    const outline = (nodes: NonNullable<SummaryResult['outline']>, depth: number) => {
      for (const node of nodes) {
        lines.push(`${'  '.repeat(depth)}- ${text(node.title)}${node.start !== undefined ? ` · ${node.start} 秒` : ''}`)
        if (node.children?.length) outline(node.children, depth + 1)
      }
    }
    outline(result.outline, 0)
    lines.push('')
  }
  return lines.join('\n')
}

function evidenceLink(source: SummarySource, evidence: SummaryEvidence, url: URL | null): string | undefined {
  return evidence.start !== undefined ? referenceURL(source, evidence.start) : url?.href
}

function summaryDocumentMarkdown(result: SummaryResult, source: SummarySource, readerURL: string): string {
  const reader = sourceURL(readerURL)
  if (!reader) throw new Error('阅读地址无效，请重新打开内容。')
  const url = sourceURL(source.url)
  const heading = source.title?.trim().replace(/\s+/g, ' ') || 'AI 摘要'
  const lines = [`# ${text(heading)}`, '', `内容标识：${text(source.contentId)}`, '']
  if (url) lines.push(`[原内容](<${url.href}>)`, '')
  lines.push(`[在 Tangentry 查看](<${reader.href}>)`, '', '## Topic 主题地图', '')
  if (result.document?.topic?.trim()) lines.push(text(result.document.topic.trim()), '')
  const used = new Set<string>()
  const renderEvidence = (evidence: SummaryEvidence[], indent: string) => {
    for (const value of evidence) {
      if (!value.quote?.trim()) continue
      const key = value.id ?? `${value.index ?? ''}|${value.quote}|${value.start ?? ''}|${value.end ?? ''}|${value.page ?? ''}|${(value.cueIds ?? []).join(',')}`
      if (used.has(key)) continue
      used.add(key)
      const timing = value.start !== undefined && value.end !== undefined ? ` · ${value.start}–${value.end} 秒` : value.page !== undefined ? ` · 第 ${value.page} 页` : ''
      const link = evidenceLink(source, value, url)
      lines.push(`${indent}- 证据${timing}`, '')
      if (link) lines.push(`${indent}  [打开原内容](<${link}>)`, '')
      lines.push(...text(value.quote).split('\n').map(line => `${indent}  > ${line}`), '')
    }
  }
  const renderUnit = (unit: SummaryUnit, depth: number) => {
    const indent = '  '.repeat(depth)
    if (unit.topic?.trim()) lines.push(`${indent}- **Topic**：${text(unit.topic.trim())}`)
    if (unit.takeaway?.trim()) lines.push(`${indent}  - **Takeaway**：${text(unit.takeaway.trim())}`)
    if (unit.key?.trim() || unit.reason?.trim()) lines.push(`${indent}  - **Key**：${text((unit.key ?? unit.reason ?? '').trim())}`)
    const brief = (unit.sentences ?? []).filter(sentence => !unit.briefSentenceIds?.length || (sentence.id && unit.briefSentenceIds.includes(sentence.id))).slice(0, 3).map(sentence => sentence.text.trim()).filter(Boolean)
    if (brief.length) lines.push(`${indent}  - **Brief**：${text(brief.join(' '))}`)
    const full = unit.full?.trim() || (unit.sentences ?? []).map(sentence => sentence.text.trim()).filter(Boolean).join('\n')
    if (full) lines.push(`${indent}  - **Full**：${text(full)}`)
    renderEvidence([...(unit.evidence ?? []), ...(unit.source ?? []), ...(unit.sentences ?? []).flatMap(sentence => sentence.evidence ?? [])], `${indent}  `)
    lines.push('')
  }
  const renderSection = (section: SummarySection, depth: number) => {
    const prefix = '#'.repeat(Math.min(6, depth + 3))
    lines.push(`${prefix} ${text(section.title)}`, '')
    if (section.topic?.trim()) lines.push(`**Topic**：${text(section.topic.trim())}`, '')
    if (section.takeaway?.trim()) lines.push(`**Takeaway**：${text(section.takeaway.trim())}`, '')
    if (section.start !== undefined && section.end !== undefined) lines.push(`时间：${section.start}–${section.end} 秒`, '')
    if (section.page !== undefined) lines.push(`页码：第 ${section.page} 页`, '')
    for (const unit of section.units ?? []) renderUnit(unit, depth)
    renderEvidence([...(section.evidence ?? []), ...(section.source ?? [])], '  ')
    for (const child of section.children ?? []) renderSection(child, depth + 1)
  }
  for (const section of result.document!.sections) renderSection(section, 0)
  if (used.size) lines.push('## 原文证据', '', `共 ${used.size} 条可定位证据。`, '')
  return lines.join('\n')
}
