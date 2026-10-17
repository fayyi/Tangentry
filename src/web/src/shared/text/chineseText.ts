import type { SubtitleTrack } from '../api/types'

export function isChineseTrack(track?: SubtitleTrack): boolean {
  if (!track) return false
  const language = track.language.toLowerCase().replace(/^ai-/, '').replaceAll('_', '-')
  if (/^(zh|cmn|zho|chi)(-|$)/.test(language)) return true
  // Unknown or incorrectly labelled transcripts can still contain Chinese.
  const count = Math.min(20, track.cues.length)
  const sample = Array.from({ length: count }, (_, index) => track.cues[count > 1 ? Math.floor(index * (track.cues.length - 1) / (count - 1)) : 0].text).join('')
  return isChineseText(sample)
}

export function isChineseText(text: string): boolean {
  let sample = '', count = 0
  for (const character of text) {
    if (count++ === 4000) break
    sample += character
  }
  const han = sample.match(/\p{Script=Han}/gu)?.length ?? 0
  const latin = sample.match(/\p{Script=Latin}/gu)?.length ?? 0
  const kana = sample.match(/[\p{Script=Hiragana}\p{Script=Katakana}]/gu)?.length ?? 0
  return kana === 0 && han >= 4 && han / (han + latin) >= 0.6
}
