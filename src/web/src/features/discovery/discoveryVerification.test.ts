import { expect, test } from 'vitest'
import { canPollPlatformVerification } from './discoveryVerification'

test('platform verification polling stops when discovery is hidden or exhausted', () => {
  expect(canPollPlatformVerification(true, 0)).toBe(true)
  expect(canPollPlatformVerification(false, 0)).toBe(false)
  expect(canPollPlatformVerification(true, 120)).toBe(false)
})
