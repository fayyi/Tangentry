import { describe, expect, it } from 'vitest'
import { matchesSubtitleLanguage, preferredSubtitleLanguage, subtitleTrackMatchesTarget } from './subtitleLanguageMatch'
import { subtitlePriority } from './useSubtitles'

describe('subtitle language matching', () => {
  it('normalizes aliases but keeps Simplified and Traditional Chinese separate', () => {
    expect(matchesSubtitleLanguage('ai-EN_us', 'en')).toBe(true)
    expect(matchesSubtitleLanguage('zh-CN', 'zh-Hans')).toBe(true)
    expect(matchesSubtitleLanguage('zh-TW', 'zh-Hans')).toBe(false)
    expect(matchesSubtitleLanguage('zh', 'zh-Hant')).toBe(false)
    expect(matchesSubtitleLanguage('zh', 'zh')).toBe(true)
  })

  it('prefers CC over AI when multiple platform tracks match', () => {
    expect(preferredSubtitleLanguage([
      { language: 'ai-en', label: 'English AI', automatic: true, source: 'ai', hasUrl: true },
      { language: 'en-US', label: 'English CC', automatic: false, source: 'cc', hasUrl: true },
    ], 'en')?.language).toBe('en-US')
  })

  it('defaults to Simplified CC, then Simplified AI, without preferring Traditional CC', () => {
    const values = [
      { language: 'zh-TW', label: '繁体中文', automatic: false, source: 'cc' },
      { language: 'ai-zh-Hans', label: '简体 AI', automatic: true, source: 'ai' },
      { language: 'zh-CN', label: '简体 CC', automatic: false, source: 'cc' },
    ]
    expect(values.slice().sort((left, right) => subtitlePriority(left) - subtitlePriority(right)).map(value => value.language)).toEqual(['zh-CN', 'ai-zh-Hans', 'zh-TW'])
  })

  it('does not treat an unspecified Chinese track as an explicit script match', () => {
    expect(matchesSubtitleLanguage('zh', 'zh-Hans')).toBe(false)
    expect(matchesSubtitleLanguage('zh', 'zh-Hant')).toBe(false)
    expect(matchesSubtitleLanguage('zh', 'zh')).toBe(true)
  })

  it('keeps the legacy Chinese-text default without letting explicit Traditional or ambiguous zh bypass translation', () => {
    const cues = [{ start: 0, end: 1, text: '这是一段足够长的中文原文。' }]
    expect(subtitleTrackMatchesTarget({ language: 'und', label: '未知语言', automatic: false, cues }, 'zh-Hans')).toBe(true)
    expect(subtitleTrackMatchesTarget({ language: 'zh-TW', label: '繁体中文', automatic: false, cues }, 'zh-Hans')).toBe(false)
    expect(subtitleTrackMatchesTarget({ language: 'zh', label: '中文', automatic: false, cues }, 'zh-Hans')).toBe(false)
    expect(subtitleTrackMatchesTarget({ language: 'en', label: '英语', automatic: false, cues }, 'zh-Hans')).toBe(false)
  })
})
