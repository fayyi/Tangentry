import { expect, test } from 'vitest'
import { cardSizeChanged } from './CardCollection'

test('reuses a card measurement when its box is unchanged', () => {
  expect(cardSizeChanged(undefined, 240, 320)).toBe(true)
  expect(cardSizeChanged({ width: 240, height: 320 }, 240, 320)).toBe(false)
  expect(cardSizeChanged({ width: 240, height: 320 }, 241, 320)).toBe(true)
  expect(cardSizeChanged({ width: 240, height: 320 }, 240, 321)).toBe(true)
})
