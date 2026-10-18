import { describe, expect, it } from 'vitest'
import { defaultFontSettings, fontSettingsKey, readFontSettings, readingFontStacks, validateFontSettings, writeFontSettings } from './useFontSettings'

function storage(initial?: string) {
  let value = initial ?? null
  return {
    getItem: () => value,
    setItem: (_key: string, next: string) => { value = next },
  }
}

describe('font settings', () => {
  it('uses the selected defaults when no preference is stored', () => {
    expect(readFontSettings(storage())).toEqual(defaultFontSettings)
  })

  it('writes a complete preference and reads it back', () => {
    const target = storage()
    const value = { ...defaultFontSettings, ui: 'noto' as const, size: 'large' as const, lineHeight: 'relaxed' as const }
    expect(writeFontSettings(value, target)).toEqual(value)
    expect(readFontSettings(target)).toEqual(value)
  })

  it('repairs invalid, partial and malformed values', () => {
    expect(validateFontSettings({ ui: 'unknown', reading: 'wenkai', size: 'tiny', lineHeight: 'relaxed' })).toEqual({ ...defaultFontSettings, reading: 'wenkai', lineHeight: 'relaxed' })
    expect(validateFontSettings({ ui: {}, reading: [], size: null, lineHeight: 1 })).toEqual(defaultFontSettings)
    expect(readFontSettings(storage('{bad json'))).toEqual(defaultFontSettings)
    expect(readFontSettings({ getItem: key => key === fontSettingsKey ? JSON.stringify({ reading: 'noto' }) : null, setItem: () => {} })).toEqual({ ...defaultFontSettings, reading: 'noto' })
  })

  it('uses a Latin-first stack for every reading font choice', () => {
    for (const stack of Object.values(readingFontStacks)) expect(stack.startsWith("'Inter'")).toBe(true)
  })
})
