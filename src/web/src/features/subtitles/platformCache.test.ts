import { afterEach, describe, expect, it, vi } from 'vitest'
import { cacheSubtitles, cachedSubtitles } from './platformCache'
import type { SubtitleDocument } from '../../shared/api/types'

const values = new Map<string, string>()
const storage = {
  getItem: (key: string) => values.get(key) ?? null,
  setItem: (key: string, value: string) => { values.set(key, value) },
  removeItem: (key: string) => { values.delete(key) },
}

describe('B站字幕平台缓存', () => {
  afterEach(() => {
    values.clear()
    vi.unstubAllGlobals()
  })

  it('保留逐分 P 状态并只缓存已验证的字幕字段', () => {
    vi.stubGlobal('sessionStorage', storage)
    const cue = { start: 0, end: 1, text: '课程字幕内容' }
    const track = {
      language: 'zh-Hans', label: '中文 AI', automatic: true, source: 'ai', mismatch: true,
      cues: [cue], subtitleUrl: 'https://aisubtitle.hdslb.com/private.json', arbitrary: 'untrusted-track-field',
    }
    const document = {
      videoId: 'BV17x411w7KC', part: 1, status: 'ai_mismatch',
      tracks: [track], languages: [{ language: 'zh-Hans', label: '中文 AI', automatic: true, source: 'ai', hasUrl: true, url: 'https://aisubtitle.hdslb.com/private.json' }],
      parts: [
        { cid: 123, page: 1, title: '第一课', status: 'cc', aiMismatch: true, languages: [], tracks: [track] },
      ],
      cookie: 'synthetic-secret', raw: { subtitle_url: 'https://aisubtitle.hdslb.com/private.json' },
    } as unknown as SubtitleDocument

    cacheSubtitles('/subtitles/bilibili?q=BV17x411w7KC', document)

    const cached = cachedSubtitles('/subtitles/bilibili?q=BV17x411w7KC', 'BV17x411w7KC')
    expect(cached?.status).toBe('ai_mismatch')
    expect(cached?.parts?.[0]).toMatchObject({ cid: 123, page: 1, status: 'cc', aiMismatch: true })
    expect(cached?.tracks[0]).toMatchObject({ source: 'ai', mismatch: true, cues: [cue] })
    const serialized = [...values.values()].join('\n')
    expect(serialized).not.toContain('aisubtitle.hdslb.com')
    expect(serialized).not.toContain('synthetic-secret')
    expect(serialized).not.toContain('untrusted-track-field')
    expect(serialized).not.toContain('"raw"')
  })

  it('拒绝缓存含凭据的错误消息', () => {
    vi.stubGlobal('sessionStorage', storage)
    const document: SubtitleDocument = {
      videoId: 'BV17x411w7KD', part: 1, status: 'error', errorMessage: 'Cookie=synthetic-secret', tracks: [],
    }

    cacheSubtitles('/subtitles/bilibili?q=BV17x411w7KD', document)

    expect(cachedSubtitles('/subtitles/bilibili?q=BV17x411w7KD', 'BV17x411w7KD')).toBeUndefined()
    expect([...values.values()].join('\n')).not.toContain('synthetic-secret')
  })

  it('不缓存限流状态，避免冷却结束后继续显示旧结果', () => {
    vi.stubGlobal('sessionStorage', storage)
    const document: SubtitleDocument = {
      videoId: 'BV17x411w7KE', part: 1, status: 'rate_limited', tracks: [],
      parts: [{ cid: 456, page: 1, status: 'rate_limited', languages: [], tracks: [] }],
    }
    cacheSubtitles('/subtitles/bilibili?q=BV17x411w7KE', document)
    expect(cachedSubtitles('/subtitles/bilibili?q=BV17x411w7KE', 'BV17x411w7KE')).toBeUndefined()
  })
})
