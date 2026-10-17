import type { SubtitleCue } from '../../shared/api/types'

const cjk = '[\\p{Script=Han}\\p{Script=Hiragana}\\p{Script=Katakana}]'
const cjkWrap = new RegExp(`(${cjk})\\s+(?=${cjk})`, 'gu')

export function transcriptText(text: string): string {
  return text.replace(/\s+/gu, ' ').replace(cjkWrap, '$1').trim()
}

export function transcriptSeparator(previous: string, next: string): string {
  return new RegExp(`${cjk}$`, 'u').test(previous) && new RegExp(`^${cjk}|^[，。！？；：、）]`, 'u').test(next) ? '' : ' '
}

// Paragraphs retain the exact cue objects so playback, search and cached
// translations keep their original indices while presentation flows as prose.
export function transcriptParagraphs(cues: SubtitleCue[]): SubtitleCue[][] {
  const paragraphs: SubtitleCue[][] = []
  let current: SubtitleCue[] = []
  let length = 0
  for (const cue of cues) {
    const previous = current.at(-1)
    if (previous && (cue.start - previous.end >= 2 || length >= 500 ||
      length >= 180 && /[.!?。！？]["'”’）)]?$/u.test(transcriptText(previous.text)) ||
      length >= 260 && cue.end - current[0].start >= 40)) {
      paragraphs.push(current)
      current = []
      length = 0
    }
    current.push(cue)
    length += transcriptText(cue.text).length
  }
  if (current.length) paragraphs.push(current)
  return paragraphs
}
