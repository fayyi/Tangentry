import type { DownloadOptions } from '../../shared/api/types'
import type { FavoritePolicy } from './useFavoriteSync'

export function sourcePolicyDraft(policy: FavoritePolicy): FavoritePolicy {
  return {
    ...policy,
    download: {
      mode: 'video',
      container: 'mp4',
      compress: false,
      subtitleLanguages: [],
      automaticSubtitles: false,
      ...policy.download,
    },
    downloadSubtitles: policy.downloadSubtitles ?? false,
  }
}

export function syncPolicyDraft(draft: FavoritePolicy, policy: FavoritePolicy, dirty: boolean): FavoritePolicy {
  return dirty ? draft : sourcePolicyDraft(policy)
}

export function parseSubtitleLanguages(text: string): string[] {
  const languages = text.split(/[，,\s]+/).map(value => value.trim()).filter(Boolean)
  return languages.length ? languages : ['all']
}

export function subtitleLanguageText(options: DownloadOptions): string {
  return options.subtitleLanguages?.filter(language => language !== 'all').join(', ') ?? ''
}
