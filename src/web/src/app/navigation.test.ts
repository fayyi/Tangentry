import { expect, test } from 'vitest'
import type { ContentItem } from '../shared/api/types'
import { appPageLayers, readerPath, readerPathForItem, type View } from './navigation'

const item = (overrides: Partial<ContentItem>): ContentItem => ({
  id: 'bilibili:dynamic:123', platform: 'bilibili', platformId: 'dynamic:123', kind: 'article',
  title: '动态', author: 'UP主', authorId: '1', url: 'https://www.bilibili.com/opus/123', thumbnailUrl: '', description: '', durationSeconds: 0, publishedAt: 0,
  ...overrides,
})

test('B站视频使用 video 查询参数，其他动态使用 contentId', () => {
  expect(readerPathForItem(item({ id: 'bilibili:BV1abCDefgH1', platformId: 'BV1abCDefgH1', kind: 'video' }))).toBe('/reader?video=BV1abCDefgH1')
  expect(readerPathForItem(item({ id: 'bilibili:dynamic:video-like', platformId: 'dynamic:video-like', kind: 'video' }))).toBe('/reader?contentId=bilibili%3Adynamic%3Avideo-like')
  expect(readerPathForItem(item({}))).toBe('/reader?contentId=bilibili%3Adynamic%3A123')
})

test('动态内容的笔记链接不会被误判为视频', () => {
  expect(readerPath('bilibili:dynamic:123', '0123456789abcdef0123456789abcdef')).toBe('/reader?contentId=bilibili%3Adynamic%3A123&note=0123456789abcdef0123456789abcdef')
  expect(readerPath('bilibili:BV1abCDefgH1')).toBe('/reader?video=BV1abCDefgH1')
})

test('页面层将保活发现页与唯一活动页面分开', () => {
  const journey: View[] = ['discovery', 'library', 'subscriptions', 'notes', 'library']
  const actual = journey.map(view => appPageLayers(view, true))
  expect(actual.map(layer => layer.activePage)).toEqual([null, 'library', 'subscriptions', 'notes', 'library'])
  expect(actual.map(layer => layer.discoveryVisible)).toEqual([true, false, false, false, false])
  expect(actual.every(layer => layer.mountDiscovery)).toBe(true)
  expect(appPageLayers('discovery', false).mountDiscovery).toBe(true)
})
