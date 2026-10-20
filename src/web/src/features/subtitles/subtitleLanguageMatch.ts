import type { SubtitleLanguage, SubtitleTrack } from '../../shared/api/types'
import { isChineseTrack } from '../../shared/text/chineseText'

export const translationTargets = [
  ['zh-Hans', '简体中文'], ['zh-Hant', '繁体中文'], ['en', '英语'], ['ja', '日语'], ['ko', '韩语'],
  ['fr', '法语'], ['de', '德语'], ['es', '西班牙语'], ['pt', '葡萄牙语'], ['ru', '俄语'], ['it', '意大利语'],
  ['ar', '阿拉伯语'], ['hi', '印地语'], ['th', '泰语'], ['vi', '越南语'], ['id', '印度尼西亚语'],
] as const

export function normalizeSubtitleLanguage(value: string) {
  const language = value.trim().toLowerCase().replaceAll('_', '-').replace(/^ai-/, '').replace(/-orig$/, '')
  if (['zh-hans', 'zh-cn', 'zh-sg'].includes(language) || language.startsWith('zh-hans-') || language.startsWith('zh-cn-') || language.startsWith('zh-sg-')) return 'zh-hans'
  if (['zh-hant', 'zh-tw', 'zh-hk', 'zh-mo'].includes(language) || language.startsWith('zh-hant-') || language.startsWith('zh-tw-') || language.startsWith('zh-hk-') || language.startsWith('zh-mo-')) return 'zh-hant'
  return language
}

export function matchesSubtitleLanguage(candidate: string, target: string) {
  const actual = normalizeSubtitleLanguage(candidate)
  const desired = normalizeSubtitleLanguage(target)
  if (actual === desired) return true
  if (actual.startsWith('zh') || desired.startsWith('zh')) return false
  return actual.split('-')[0] === desired.split('-')[0]
}

export function subtitleTrackMatchesTarget(track: SubtitleTrack, target: string) {
  if (matchesSubtitleLanguage(track.language, target)) return true
  if (normalizeSubtitleLanguage(target) !== 'zh-hans' || !isChineseTrack(track)) return false
  const language = normalizeSubtitleLanguage(track.language)
  // Keep the legacy Chinese-text heuristic for untagged transcripts, but an
  // explicit or ambiguous Chinese code cannot stand in for Simplified.
  return /^(?:und|unknown)$/.test(language)
}

export function preferredSubtitleLanguage<T extends SubtitleLanguage | SubtitleTrack>(values: readonly T[], target: string) {
  return values.filter(value => matchesSubtitleLanguage(value.language, target) && (!('hasUrl' in value) || value.hasUrl !== false)).sort((left, right) => {
    const priority = (value: T) => value.source === 'cc' ? 0 : value.source === 'ai' || value.automatic ? 1 : value.source === 'youtube' && value.translated ? 2 : 0
    const leftSource = priority(left)
    const rightSource = priority(right)
    return leftSource - rightSource || left.language.localeCompare(right.language)
  })[0]
}

export function subtitleTargetLabel(code: string) {
  const normalized = normalizeSubtitleLanguage(code)
  return translationTargets.find(([target]) => normalizeSubtitleLanguage(target) === normalized)?.[1] ?? code
}
