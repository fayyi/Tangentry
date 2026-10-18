import { expect, test } from 'vitest'
import { bilibiliPath, bilibiliReturnPath, bilibiliRouteUpdate, bilibiliStateFromURL } from './bilibiliNavigation'

test('B站子视图路径包含 tab，直播分区仅在直播 tab 下写入', () => {
  expect(bilibiliPath('ranking')).toBe('/bilibili?tab=ranking')
  expect(bilibiliPath('live', '9')).toBe('/bilibili?tab=live&area=9')
  expect(bilibiliPath('popular', '9')).toBe('/bilibili?tab=popular')
})

test('从 B站完整 URL 恢复 tab 和直播分区，未知值回到稳定默认值', () => {
  expect(bilibiliStateFromURL('/bilibili?tab=live&area=2')).toEqual({ tab: 'live', area: '2' })
  expect(bilibiliStateFromURL('/bilibili?tab=unknown&area=999')).toEqual({ tab: 'popular', area: '1' })
  expect(bilibiliStateFromURL('/bilibili?tab=ranking&area=9')).toEqual({ tab: 'ranking', area: '9' })
})

test('B站子视图切换交给应用导航层 replace 当前路由', () => {
  expect(bilibiliRouteUpdate('live', '2')).toEqual({ action: 'replace', path: '/bilibili?tab=live&area=2' })
})

test('进入 B站 时保留发现页完整 query/hash 来源，其他路径回到发现首页', () => {
  expect(bilibiliReturnPath('/?topic=技术&platform=bilibili&q=AI#results')).toBe('/?topic=%E6%8A%80%E6%9C%AF&platform=bilibili&q=AI#results')
  expect(bilibiliReturnPath('/search?q=AI')).toBe('/')
  expect(bilibiliReturnPath('https://example.invalid/?topic=AI')).toBe('/')
  expect(bilibiliReturnPath('//example.invalid/?topic=AI')).toBe('/')
  expect(bilibiliReturnPath()).toBe('/')
})
