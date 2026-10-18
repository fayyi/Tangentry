import { describe, expect, test } from 'vitest'
import { appendSearchItems, groupSearchResults, mergeSearchItems } from './searchLogic'

describe('unified search grouping', () => {
  test('groups sources and removes duplicate cards for mixed mode', () => {
    const result = { query: '测试', local: { items: [{ id: 'a', publishedAt: 1 } as any], total: 1, counts: {} }, sources: { bilibili: { page: { platform: 'bilibili', items: [{ id: 'a', publishedAt: 1 } as any, { id: 'b', publishedAt: 3 } as any], hasMore: false } } } }
    const groups = groupSearchResults(result, { bilibili: 'B站' })
    expect(groups.map(group => group.title)).toEqual(['本机资料库与订阅', 'B站'])
    expect(mergeSearchItems(groups).map(item => item.id)).toEqual(['b', 'a'])
  })

  test('appends only unseen cards', () => {
    expect(appendSearchItems([{ id: 'a' } as any], [{ id: 'a' } as any, { id: 'b' } as any]).map(item => item.id)).toEqual(['a', 'b'])
  })
})
