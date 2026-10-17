import type { SubtitleTrack } from '../../shared/api/types'

export type TranscriptExportMode = 'original' | 'bilingual' | 'translated'
export interface TranscriptSource {
  contentId: string
  title?: string
  author?: string
  url: string
}

function literal(value: string): string {
  return value.replaceAll('&', '&amp;').replace(/[\\`*_{}[\]()<>#+.!|~-]/g, '\\$&')
}

function publicURL(value: string): URL | undefined {
  try {
    const url = new URL(value)
    if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) return url
  } catch { /* Omit a source link when the supplied address is invalid. */ }
}

function destination(url: URL): string {
  return url.href.replace(/[<>\s\\]/g, character => encodeURIComponent(character))
}

function originalAt(source: TranscriptSource, seconds: number): URL | undefined {
  const url = publicURL(source.url)
  if (!url) return
  if (source.contentId.startsWith('youtube:') && url.hostname === 'www.youtube.com' && url.pathname === '/watch' &&
    url.searchParams.get('v') === source.contentId.slice('youtube:'.length)) {
    url.searchParams.set('t', `${Math.floor(seconds)}s`)
    return url
  }
  if (source.contentId.startsWith('bilibili:') && url.hostname === 'www.bilibili.com' &&
    url.pathname.replace(/\/$/, '') === `/video/${source.contentId.slice('bilibili:'.length)}`) {
    // This reader currently loads the first part, even if a source URL names another.
    url.searchParams.set('p', '1')
    url.searchParams.set('t', String(Math.floor(seconds)))
    return url
  }
}

function clock(seconds: number): string {
  const milliseconds = Math.round(seconds * 1000)
  const hours = Math.floor(milliseconds / 3600000)
  const minutes = Math.floor(milliseconds / 60000) % 60
  const secs = Math.floor(milliseconds / 1000) % 60
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}.${String(milliseconds % 1000).padStart(3, '0')}`
}

export function transcriptMarkdown(track: SubtitleTrack, source: TranscriptSource, readerURL: string,
  mode: TranscriptExportMode = 'original', translations?: readonly string[]): string {
  const reader = publicURL(readerURL)
  if (!reader || !track.cues.length) throw new Error('文稿不可用，请重新读取。')
  if (mode === 'translated' && !track.cues.every((_, index) => translations?.[index]?.trim())) {
    throw new Error('译文尚未完成，可先导出双语文稿。')
  }
  const original = publicURL(source.url)
  const title = source.title?.trim().replace(/\s+/g, ' ') || '字幕文稿'
  const lines = [`# ${literal(title)}`, '', `内容标识：${literal(source.contentId)}`, '']
  if (source.author?.trim()) lines.push(`作者：${literal(source.author.trim().replace(/\s+/g, ' '))}`, '')
  lines.push(`语言：${literal(track.label)}（${literal(track.language)}）`, '',
    `文稿：${mode === 'original' ? '原文' : mode === 'bilingual' ? '双语' : '译文'} · ${track.cues.length} 段`, '')
  if (mode === 'bilingual') {
    const ready = track.cues.filter((_, index) => translations?.[index]?.trim()).length
    lines.push(`已翻译 ${ready} / ${track.cues.length} 段；未翻译段落保留原文。`, '')
  }
  if (original) lines.push(`[原内容](<${destination(original)}>)`, '')
  lines.push(`[在 Tangentry 查看](<${destination(reader)}>)`, '', '## 文稿', '')
  for (const [index, cue] of track.cues.entries()) {
    if (!Number.isFinite(cue.start) || !Number.isFinite(cue.end) || cue.start < 0 || cue.end <= cue.start || !cue.text.trim()) {
      throw new Error('字幕时间或文字不完整，请重新读取。')
    }
    const at = originalAt(source, cue.start)
    lines.push(`### ${index + 1} · ${clock(cue.start)} – ${clock(cue.end)}`, '',
      `时间：${cue.start}–${cue.end} 秒`, '')
    if (at) lines.push(`[跳转原视频](<${destination(at)}>)`, '')
    const translated = translations?.[index]?.trim()
    const bodies = mode === 'translated' ? [translated!] :
      mode === 'bilingual' && translated && translated !== cue.text.trim() ? [cue.text, translated] : [cue.text]
    for (const body of bodies) lines.push(...literal(body).split(/\r\n|\r|\n/).map(line => `> ${line}`), '')
  }
  return lines.join('\n')
}
