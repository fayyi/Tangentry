import { expect, test } from 'vitest'
import { cardSizeChanged, masonryIntrinsicSize, masonryNeedsMeasurement } from './CardCollection'

test('reuses a card measurement when its box is unchanged', () => {
  expect(cardSizeChanged(undefined, 240, 320)).toBe(true)
  expect(cardSizeChanged({ width: 240, height: 320 }, 240, 320)).toBe(false)
  expect(cardSizeChanged({ width: 240, height: 320 }, 241, 320)).toBe(true)
  expect(cardSizeChanged({ width: 240, height: 320 }, 240, 321)).toBe(true)
})

test('formats a measured masonry height as an intrinsic size', () => {
  expect(masonryIntrinsicSize(321.2)).toBe('auto 322px')
  expect(masonryIntrinsicSize(-1)).toBe('auto 0px')
})

test('only measures cards missing geometry or when spacing changes', () => {
  expect(masonryNeedsMeasurement(120, 0, false)).toBe(true)
  expect(masonryNeedsMeasurement(120, 120, false)).toBe(false)
  expect(masonryNeedsMeasurement(120, 119, false)).toBe(true)
  expect(masonryNeedsMeasurement(120, 120, true)).toBe(true)
})
