import { expect, test } from 'vitest'
import type { DiscoveryBatchItem } from '../../shared/api/types'
import { appendDiscoveryBatch, appendDiscoveryItems, buildDiscoveryBatchParams, buildDiscoveryTopicParam, discoveryReasonType, discoverySourceLabel } from './useDiscoveryBatch'

function entry(id: string, source = 'bilibili', reason = '来源更新'): DiscoveryBatchItem {
  return {
    item: {
      id, platform: source, platformId: id, kind: 'video', title: id, author: '作者', authorId: 'author',
      url: `https://example.com/${id}`, thumbnailUrl: '', description: '', durationSeconds: 0, publishedAt: 1,
    },
    source, reason, carriedOver: false,
  }
}

test('appends cursor items in order and removes duplicate content IDs', () => {
  const current = [entry('a'), entry('b')]
  const next = [entry('b', 'youtube'), entry('c')]
  expect(appendDiscoveryItems(current, next).map(value => value.item.id)).toEqual(['a', 'b', 'c'])
  expect(current).toHaveLength(2)
})

test('merges cursor response metadata while keeping accumulated items', () => {
  const first = { batchId: 'batch', items: [entry('a')], hasNextBatch: true, nextCursor: 'cursor-2', reason: 'x', sourceStatus: { bilibili: { status: 'ok' } } }
  const second = { batchId: 'batch', items: [entry('a'), entry('b')], hasNextBatch: false, reason: 'y', sourceStatus: { youtube: { status: 'ok' } } }
  const merged = appendDiscoveryBatch(first, second)
  expect(merged.items.map(value => value.item.id)).toEqual(['a', 'b'])
  expect(merged.hasNextBatch).toBe(false)
  expect(merged.sourceStatus).toEqual({ bilibili: { status: 'ok' }, youtube: { status: 'ok' } })
  expect(first.items).toHaveLength(1)
})

test('builds topic and cursor parameters without exposing finite-batch controls', () => {
  expect(buildDiscoveryTopicParam('  科技  ')).toBe('科技')
  expect(buildDiscoveryTopicParam('  ')).toBeUndefined()
  expect(buildDiscoveryBatchParams({ view: 'wander', topic: ' 科技 ', source: '  ' }, 'next', 7, ['a', 'a', ''])).toEqual({
    view: 'wander', topic: '科技', cursor: 'next', seed: 7, unreadIds: ['a'],
  })
})

test('maps source and reason to compact Chinese labels', () => {
  expect(discoverySourceLabel('subscriptions')).toBe('订阅更新')
  expect(discoverySourceLabel('bilibili')).toBe('B站推荐')
  expect(discoverySourceLabel('unknown')).toBe('unknown 推荐')
  expect(discoverySourceLabel('youtube', '少量随机探索')).toBe('陌生探索')
})

test('maps backend reasons to discovery badge types', () => {
  expect(discoveryReasonType('subscriptions', '最近更新')).toBe('订阅')
  expect(discoveryReasonType('bilibili', '订阅优先')).toBe('订阅')
  expect(discoveryReasonType('youtube', '少量随机探索')).toBe('探索')
  expect(discoveryReasonType('linuxdo', '平台首页推荐')).toBe('推荐')
})
