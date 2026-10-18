import { describe, expect, it } from 'vitest'
import { hasPlayableOnlineSource, shouldStartCompatibilityPlayback } from './onlinePlayback'

describe('hasPlayableOnlineSource', () => {
  it('requires a real source before rendering native video', () => {
    expect(hasPlayableOnlineSource()).toBe(false)
    expect(hasPlayableOnlineSource('', '')).toBe(false)
    expect(hasPlayableOnlineSource('', '/api/playback/compatibility/job/media')).toBe(true)
    expect(hasPlayableOnlineSource('https://cdn.example/video.mp4')).toBe(true)
  })
})

describe('shouldStartCompatibilityPlayback', () => {
  it('keeps Xiaohongshu fallback while isolating Douyin sessions', () => {
    expect(shouldStartCompatibilityPlayback('xiaohongshu')).toBe(true)
    expect(shouldStartCompatibilityPlayback('douyin')).toBe(false)
    expect(shouldStartCompatibilityPlayback('bilibili')).toBe(false)
  })
})
