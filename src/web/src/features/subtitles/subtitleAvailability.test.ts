import { describe, expect, it } from 'vitest'
import { selectedSubtitleAvailability, subtitleDocumentMessage, subtitlePartMessage } from './subtitleAvailability'
import { subtitlePriority } from './useSubtitles'
import type { SubtitleDocument, SubtitlePart, SubtitleTrack } from '../../shared/api/types'

describe('B站字幕可用性', () => {
  it('优先中文 CC、中文 AI，再到其他语言', () => {
    const tracks = [
      { language: 'en', source: 'cc' },
      { language: 'zh-Hans', source: 'ai' },
      { language: 'zh-CN', source: 'cc' },
    ]
    expect([...tracks].sort((a, b) => subtitlePriority(a) - subtitlePriority(b)).map(track => track.language)).toEqual(['zh-CN', 'zh-Hans', 'en'])
  })

  it('把空地址、无字幕和接口错误导向不同下一步', () => {
    const part = (status: SubtitlePart['status'], errorCode?: number, errorMessage?: string): SubtitlePart => ({ cid: 12, page: 1, status, languages: [], tracks: [], errorCode, errorMessage })
    expect(subtitlePartMessage(part('api_empty'))).toMatchObject({ action: '去账号设置', tone: 'warning' })
    expect(subtitlePartMessage(part('none'))).toMatchObject({ action: '转录', tone: 'neutral' })
    expect(subtitlePartMessage(part('error', -101, '需要登录'))).toMatchObject({ message: expect.stringContaining('-101'), action: '重试', tone: 'danger' })
    expect(subtitlePartMessage(part('rate_limited', 412, 'HTTP 412 text/html'))).toMatchObject({ message: expect.stringContaining('412'), action: '稍后重试', tone: 'warning' })
    expect(subtitleDocumentMessage('rate_limited', 412, 'HTTP 412 text/html')).toContain('412')
    expect(subtitleDocumentMessage('api_empty')).toContain('地址为空')
    expect(subtitleDocumentMessage('error', -101, '需要登录')).toContain('需要登录')
    expect(subtitlePartMessage(part('ai_mismatch'))).toMatchObject({ message: expect.stringContaining('仍可查看'), action: '改用转录' })
  })

  it('混合 CC 与 AI 时仍显示 AI 存疑标记', () => {
    const mixed = { cid: 12, page: 1, status: 'cc' as const, aiMismatch: true, languages: [], tracks: [] }
    expect(subtitlePartMessage(mixed)).toMatchObject({ message: expect.stringContaining('另有 AI 字幕疑似错位'), tone: 'warning' })
  })

  it('注明通过 yt-dlp 读取的字幕来源', () => {
    const part = { cid: 12, page: 1, status: 'ai' as const, languages: [], tracks: [{ language: 'zh-CN', label: '简体中文', automatic: true, source: 'yt-dlp' as const, cues: [] }] }
    expect(subtitlePartMessage(part)).toMatchObject({ message: expect.stringContaining('yt-dlp') })
  })

  it('本机副本不掩盖当前平台 AI 轨道的存疑标记', () => {
    const platformTrack: SubtitleTrack = { language: 'zh-Hans', label: '中文 AI', automatic: true, source: 'ai', mismatch: true, cues: [{ start: 0, end: 1, text: '字幕内容' }] }
    const savedTrack: SubtitleTrack = { ...platformTrack, mismatch: false }
    const doc: SubtitleDocument = { videoId: 'BV17x411w7KC', part: 1, status: 'ai_mismatch', tracks: [platformTrack], parts: [{ cid: 12, page: 1, status: 'ai_mismatch', languages: [], tracks: [platformTrack] }] }
    expect(selectedSubtitleAvailability(doc, savedTrack, 'saved')).toBe('ai_mismatch')
    expect(selectedSubtitleAvailability(doc, { ...savedTrack, source: 'cc' }, 'saved')).toBe('available')
  })
})
