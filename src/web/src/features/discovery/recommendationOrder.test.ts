import { expect, test } from 'vitest'
import { isCachedContinuation, mixRecommendations } from './recommendationOrder'
import type { ContentItem, Feed } from '../../shared/api/types'

const item = (id: string, platform: string): ContentItem => ({ id, platform, platformId: id, kind: 'video', title: id, author: '', authorId: '', url: '', thumbnailUrl: '', description: '', durationSeconds: 0, publishedAt: 0 })
const source = (platform: string, items: ContentItem[]) => ({ source: platform, label: platform, feed: { items, page: 1, total: items.length } as Feed })

test('cache and its first remote page retain the prefix while remote page-one refreshes replace it', () => {
  const previous = { ...source('youtube', [item('a', 'youtube'), item('b', 'youtube')]).feed, page: 0 }
  const next = { ...previous, items: [...previous.items, item('c', 'youtube')] }
  expect(isCachedContinuation(previous, next)).toBe(true)
  expect(isCachedContinuation(previous, { ...next, page: 1 })).toBe(true)
  expect(isCachedContinuation({ ...previous, page: 1 }, { ...next, page: 1 })).toBe(false)
  expect(isCachedContinuation(previous, { ...next, items: [...next.items].reverse() })).toBe(false)
  expect(isCachedContinuation(previous, { ...next, items: next.items.slice(1) })).toBe(false)
})

test('deduplication compacts source queues before alternating and never mutates input feeds', () => {
  const a = item('a', 'bilibili'), b = item('b', 'bilibili')
  const feeds = [source('bilibili', [a, a, b]), source('youtube', [item('c', 'youtube'), item('d', 'youtube')])]
  const before = JSON.stringify(feeds)
  expect(mixRecommendations(feeds).map(entry => entry.item.id)).toEqual(['a', 'c', 'b', 'd'])
  for (let run = 0; run < 20; run++) {
    const result = mixRecommendations(feeds, true)
    expect(new Set(result.map(entry => entry.item.id)).size).toBe(4)
    for (let index = 0; index < result.length; index += 2) {
      expect(new Set(result.slice(index, index + 2).map(entry => entry.item.platform))).toEqual(new Set(['bilibili', 'youtube']))
    }
  }
  expect(JSON.stringify(feeds)).toBe(before)
})

test('shared identities appear once and exhausted sources do not discard remaining recommendations', () => {
  const shared = item('shared', 'bilibili')
  const result = mixRecommendations([source('bilibili', [shared]), source('youtube', [shared, item('c', 'youtube'), item('d', 'youtube')])])
  expect(result.map(entry => entry.item.id)).toEqual(['shared', 'c', 'd'])
  expect(mixRecommendations([])).toEqual([])
})
