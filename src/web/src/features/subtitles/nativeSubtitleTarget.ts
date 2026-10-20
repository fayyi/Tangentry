import type { SubtitleLanguage, SubtitleTrack } from '../../shared/api/types'

export type SubtitleLanguageRead = { language: string; status: 'loading' | 'loaded' | 'error'; error?: string }

export function resolveNativeSubtitleTarget(
  target: SubtitleLanguage | undefined,
  tracks: readonly SubtitleTrack[],
  read: SubtitleLanguageRead | null,
  fallbackTrack?: SubtitleTrack,
) {
  if (!target) return { track: undefined, displayTrack: fallbackTrack, status: 'none' as const, failed: false }
  const track = tracks.find(candidate => candidate.language === target.language)
  if (track) return { track, displayTrack: track, status: 'loaded' as const, failed: false }
  if (read?.language === target.language && read.status === 'error') {
    return { track: undefined, displayTrack: fallbackTrack, status: 'error' as const, failed: true }
  }
  return { track: undefined, displayTrack: fallbackTrack, status: 'loading' as const, failed: false }
}

export function availableSubtitleLanguages(languages: readonly SubtitleLanguage[], failedLanguage?: string) {
  return failedLanguage ? languages.filter(language => language.language !== failedLanguage) : [...languages]
}
