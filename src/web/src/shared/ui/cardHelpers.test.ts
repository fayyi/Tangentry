import { describe, expect, test } from 'vitest'
import { creatorViewSupported, platformColorToken, platformLabel } from './cardHelpers'

describe('platform metadata', () => {
  test('maps known platform names and theme tokens', () => {
    expect(platformLabel('bilibili')).toBe('B站')
    expect(platformLabel('linuxdo')).toBe('Linux.do')
    expect(platformColorToken('youtube')).toBe('--youtube-red')
    expect(platformColorToken('rss')).toBe('--rss-orange')
  })

  test('uses the neutral token for unknown platforms', () => {
    expect(platformLabel('custom-feed')).toBe('custom-feed')
    expect(platformColorToken('custom-feed')).toBe('--platform-neutral')
  })
})

describe('creator view platform support', () => {
  test('supports creator views only on platforms with creator feeds', () => {
    expect(creatorViewSupported('bilibili')).toBe(true)
    expect(creatorViewSupported('youtube')).toBe(true)
    expect(creatorViewSupported('linuxdo')).toBe(true)
    expect(creatorViewSupported('xiaoyuzhou')).toBe(true)
    expect(creatorViewSupported('douyin')).toBe(false)
    expect(creatorViewSupported('xiaohongshu')).toBe(false)
  })
})
