import { describe, expect, it } from 'vitest'
import { feedbackMessage, feedbackValue } from './discoveryFeedback'

const item = { id: 'youtube:abc', platform: 'youtube', platformId: 'abc', kind: 'video', title: '标题', author: '作者', authorId: 'creator-1', category: '游戏', url: 'https://example.com', thumbnailUrl: '', description: '', durationSeconds: 0, publishedAt: 0 } as const

describe('discovery feedback helpers', () => {
  it('uses stable item dimensions as feedback targets', () => {
    expect(feedbackValue(item, 'item')).toBe('youtube:abc')
    expect(feedbackValue(item, 'source')).toBe('youtube')
    expect(feedbackValue(item, 'creator')).toBe('creator-1')
    expect(feedbackValue(item, 'topic')).toBe('游戏')
  })

  it('keeps feedback copy explicit and reversible', () => {
    expect(feedbackMessage('more')).toContain('多看看')
    expect(feedbackMessage('less')).toContain('7 天')
    expect(feedbackMessage('hide')).toContain('隐藏')
    expect(feedbackMessage('block_creator')).toContain('设置中解除')
  })
})
