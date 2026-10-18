import { describe, expect, it } from 'vitest'
import { hasPlayableOnlineSource } from './onlinePlayback'

describe('hasPlayableOnlineSource', () => {
  it('requires a real source before rendering native video', () => {
    expect(hasPlayableOnlineSource()).toBe(false)
    expect(hasPlayableOnlineSource('', '')).toBe(false)
    expect(hasPlayableOnlineSource('', '/api/playback/compatibility/job/media')).toBe(true)
    expect(hasPlayableOnlineSource('https://cdn.example/video.mp4')).toBe(true)
  })
})
