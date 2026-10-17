import { expect, test } from 'vitest'
import { activeSubtitleCue } from './activeCue'

test('overlapping captions select the newest started segment without shifting actual time', () => {
  const old = { start: 44, end: 50, text: '旧段' }
  const current = { start: 47, end: 49, text: '当前段' }
  const cues = [current, old]
  expect(activeSubtitleCue(cues, 46.9)).toBe(old)
  expect(activeSubtitleCue(cues, 47)).toBe(current)
  expect(activeSubtitleCue(cues, 48.9)).toBe(current)
  expect(activeSubtitleCue(cues, 49)).toBe(old)
  expect(activeSubtitleCue(cues, 50)).toBeUndefined()
  expect(activeSubtitleCue(cues, null)).toBeUndefined()
  expect(activeSubtitleCue(cues, NaN)).toBeUndefined()
})
