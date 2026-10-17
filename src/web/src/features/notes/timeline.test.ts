import { expect, test } from 'vitest'
import { dateRange } from './timeline'

test('local date range handles daylight saving and rejects impossible dates', () => {
  const previous = process.env.TZ
  process.env.TZ = 'America/Chicago'
  try {
    const spring = dateRange('2026-03-08', '2026-03-08')!
    const autumn = dateRange('2026-11-01', '2026-11-01')!
    expect(spring.to - spring.from).toBe(23 * 3600)
    expect(autumn.to - autumn.from).toBe(25 * 3600)
    expect(dateRange('2026-02-30', '')).toBeNull()
    expect(dateRange('2026-10-10', '2026-10-09')).toBeNull()
    expect(dateRange('', '')).toEqual({ from: 0, to: 0 })
  } finally { if (previous === undefined) delete process.env.TZ; else process.env.TZ = previous }
})
