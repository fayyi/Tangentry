import { describe, expect, it } from 'vitest'
import { dateFilterTimestamp, emptySavedViewFilters, localDateInputValue, relativeDateFilterTimestamp, savedViewDefinition, savedViewPath } from './savedViewDefinition'

describe('保存视图定义', () => {
  it('保存完整筛选组合且不复用可变对象', () => {
    const filters = { ...emptySavedViewFilters, query: '编译器', platform: 'youtube', kind: 'video', read: 'unread', favorite: true, sinceOffset: 'week' }
    const definition = savedViewDefinition(filters)
    filters.query = 'changed'
    expect(definition).toEqual({ version: 1, filters: { ...emptySavedViewFilters, query: '编译器', platform: 'youtube', kind: 'video', read: 'unread', favorite: true, sinceOffset: 'week' } })
  })

  it('生成可分享的侧栏视图地址并编码标识', () => {
    const id = 'view:abc123'
    expect(savedViewPath(id)).toBe('/subscriptions?view=view%3Aabc123')
  })

  it('把日期边界解释为本地日历日', () => {
    const start = new Date('2026-10-01T00:00:00').getTime() / 1000
    const end = new Date('2026-10-01T23:59:59').getTime() / 1000
    expect(dateFilterTimestamp('2026-10-01')).toBe(start)
    expect(dateFilterTimestamp('2026-10-01', true)).toBe(end)
  })

  it('计算本周与本月起点，并按本地日期回填控件', () => {
    const now = new Date(2026, 9, 17, 13, 20)
    expect(new Date(relativeDateFilterTimestamp('week', now) * 1000)).toEqual(new Date(2026, 9, 12))
    expect(new Date(relativeDateFilterTimestamp('month', now) * 1000)).toEqual(new Date(2026, 9, 1))
    expect(localDateInputValue(relativeDateFilterTimestamp('week', now))).toBe('2026-10-12')
  })
})
