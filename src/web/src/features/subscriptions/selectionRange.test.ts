import { describe, expect, it } from 'vitest'
import { rangeSelection, toggleSelection } from './selectionRange'

describe('订阅来源选择范围', () => {
  const ids = ['a', 'b', 'c', 'd']

  it('支持正向和反向 Shift 连选', () => {
    expect(rangeSelection(ids, 'b', 'd')).toEqual(['b', 'c', 'd'])
    expect(rangeSelection(ids, 'd', 'b')).toEqual(['b', 'c', 'd'])
  })

  it('锚点不存在时只选择目标', () => {
    expect(rangeSelection(ids, 'missing', 'c')).toEqual(['c'])
  })

  it('普通点击切换选择，Shift 选择不会取消已有项', () => {
    expect([...toggleSelection([], ids, 'b')]).toEqual(['b'])
    expect([...toggleSelection(['b'], ids, 'b')]).toEqual([])
    expect([...toggleSelection(['a'], ids, 'd', true, 'b')]).toEqual(['a', 'b', 'c', 'd'])
  })
})
