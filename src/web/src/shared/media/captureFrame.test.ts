import { expect, test } from 'vitest'
import { getCaptureCrop } from './captureFrame'

test('embedded player crop retains the bottom subtitle/danmu area with controlsHeight zero', () => {
  const rect = { left: 100, top: 100, width: 320, height: 180 }
  expect(getCaptureCrop(rect, 1, 1, 0, 1280, 900)).toEqual({ sx: 100, sy: 100, sw: 320, sh: 180 })
  expect(getCaptureCrop(rect, 2, 2, 0, 2560, 1800)).toEqual({ sx: 200, sy: 200, sw: 640, sh: 360 })
  expect(getCaptureCrop(rect, 1, 1, 48, 1280, 900).sh).toBe(132)
  // The bottom 32px in the source crop remains present, so subtitles/danmu
  // positioned against the iframe's lower edge are not clipped.
  expect(getCaptureCrop(rect, 1, 1, 0, 1280, 900).sy + getCaptureCrop(rect, 1, 1, 0, 1280, 900).sh).toBe(280)
})
