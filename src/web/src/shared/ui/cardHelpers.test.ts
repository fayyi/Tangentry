import { describe, expect, test } from 'vitest'
import { platformColorToken, platformLabel } from './cardHelpers'

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
