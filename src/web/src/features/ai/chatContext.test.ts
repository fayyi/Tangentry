import { describe, expect, it } from 'vitest'
import { buildChatContext } from './chatContext'

describe('buildChatContext', () => {
  it('puts summary before source material', () => {
    expect(buildChatContext('正文', '概览', ['要点'])).toBe('概览\n要点\n\n正文')
  })

  it('keeps bounded head and tail snippets for long material', () => {
    const value = buildChatContext('a'.repeat(200), '摘要', ['要点'], 80)
    expect(value.length).toBeLessThanOrEqual(80)
    expect(value).toContain('摘要')
    expect(value).toContain('相关内容片段')
    expect(value).toContain('……')
  })
})
