import type { SubtitleCue, SubtitleReference, SubtitleTrack } from '../../shared/api/types'
import { transcriptSeparator, transcriptText } from './transcriptParagraphs'

export type ExcerptRadius = 0 | 1 | 2
const preference = 'tangentry.subtitle-excerpt-radius.v1'

export function readExcerptRadius(): ExcerptRadius {
  try {
    const value = localStorage.getItem(preference)
    return value === '1' || value === '2' ? Number(value) as ExcerptRadius : 0
  } catch { return 0 }
}

export function rememberExcerptRadius(value: ExcerptRadius) {
  try { localStorage.setItem(preference, String(value)) }
  catch { /* An optional preference does not prevent saving an excerpt. */ }
}

export function subtitleExcerpt(track: SubtitleTrack, selected: readonly SubtitleCue[], radius: ExcerptRadius, trackHash: string): SubtitleReference {
  const first = track.cues.indexOf(selected[0]), last = track.cues.indexOf(selected.at(-1)!)
  if (first < 0 || last < first) throw new Error('这段字幕已变化，请重新选择。')
  // Search and pagination change the visible rows, never the nearby source cues.
  const cues = radius ? track.cues.slice(Math.max(0, first - radius), last + radius + 1) : selected
  const text = cues.map(cue => transcriptText(cue.text)).reduce((value, next) => value ? value + transcriptSeparator(value, next) + next : next, '')
  if (!text || Array.from(text).length > 2000) throw new Error('这段摘录超过 2000 字，请缩小范围。')
  return { start: cues[0].start, end: Math.max(...cues.map(cue => cue.end)), text, language: track.language, trackHash }
}
