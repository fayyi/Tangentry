import { describe, expect, it } from 'vitest'
import { parseSubtitleLanguages, sourcePolicyDraft, subtitleLanguageText, syncPolicyDraft } from './policyDraft'
import type { FavoritePolicy } from './useFavoriteSync'

const policy: FavoritePolicy = {
  enabled: true,
  autoDownload: true,
  intervalMinutes: 60,
  maxPerRun: 3,
  maxHeight: 1080,
  download: { mode: 'video', container: 'mp4', subtitleLanguages: ['all'] },
  downloadSubtitles: true,
  directoryName: '课程收藏 (12345)',
}

describe('favorite policy drafts', () => {
  it('keeps unsaved values while polling refreshes the source policy', () => {
    const draft = sourcePolicyDraft(policy)
    draft.maxHeight = 720
    draft.download = { ...draft.download, subtitleLanguages: ['zh-Hans', 'en'] }
    expect(syncPolicyDraft(draft, { ...policy, maxHeight: 1440 }, true)).toBe(draft)
    expect(draft.maxHeight).toBe(720)
    expect(draft.download.subtitleLanguages).toEqual(['zh-Hans', 'en'])
  })

  it('parses comma-separated language input and preserves the all-languages default', () => {
    expect(parseSubtitleLanguages('zh-Hans, en，ja')).toEqual(['zh-Hans', 'en', 'ja'])
    expect(parseSubtitleLanguages('')).toEqual(['all'])
    expect(subtitleLanguageText(policy.download)).toBe('')
  })
})
