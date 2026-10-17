import { describe, expect, it } from 'vitest'
import { DEFAULT_READER_LAYOUT, READER_LAYOUT_LIMITS, adjustLayoutWidth, clampLayoutWidth, readReaderLayout, writeReaderLayout } from './readerLayout'

describe('reader layout preferences', () => {
  it('clamps pane and player widths', () => {
    expect(clampLayoutWidth(100, READER_LAYOUT_LIMITS.ai)).toBe(220)
    expect(adjustLayoutWidth(300, 200, READER_LAYOUT_LIMITS.notes)).toBe(500)
    expect(clampLayoutWidth(Number.NaN, READER_LAYOUT_LIMITS.player)).toBe(45)
  })

  it('round trips and repairs persisted values', () => {
    const values = new Map<string, string>()
    const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) }
    writeReaderLayout({ ai: 300, notes: 480, player: 72 }, storage)
    expect(readReaderLayout(storage)).toEqual({ ai: 300, notes: 480, player: 72 })
    values.set('tangentry.reader-layout.v1', '{"ai":9999}')
    expect(readReaderLayout(storage)).toEqual({ ...DEFAULT_READER_LAYOUT, ai: 420 })
  })
})
