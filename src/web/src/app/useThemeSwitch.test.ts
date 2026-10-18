import { describe, expect, it } from 'vitest'
import { parseThemePreference, readInitialPreference, readLegacyPreference, resolveTheme, shouldApplyRemoteTheme, subscribeSystemThemeChanges } from './useThemeSwitch'

describe('theme preference', () => {
  it('accepts light, dark and system and rejects unknown values', () => {
    expect(parseThemePreference('light')).toBe('light')
    expect(parseThemePreference('dark')).toBe('dark')
    expect(parseThemePreference('system')).toBe('system')
    expect(parseThemePreference('neon')).toBe('system')
  })
  it('resolves only system from the media preference', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })
  it('updates and unsubscribes the system media listener', () => {
    let listener: ((event: Event) => void) | undefined
    let removed = false
    const media = {
      addEventListener: (_event: string, callback: (event: Event) => void) => { listener = callback },
      removeEventListener: (_event: string, callback: (event: Event) => void) => { removed = callback === listener },
    }
    let changes = 0
    const unsubscribe = subscribeSystemThemeChanges(media, () => { changes++ })
    listener?.(new Event('change'))
    unsubscribe()
    expect(changes).toBe(1)
    expect(removed).toBe(true)
  })
  it('reads legacy storage and tolerates unavailable storage', () => {
    expect(readLegacyPreference({ getItem: () => 'dark' })).toBe('dark')
    expect(readLegacyPreference({ getItem: () => 'invalid' })).toBeNull()
    expect(readLegacyPreference({ getItem: () => { throw new Error('storage unavailable') } })).toBeNull()
  })
  it('uses the injected stable preference before React starts', () => {
    expect(readInitialPreference({ injected: 'dark', saved: true, legacy: 'light' })).toBe('dark')
    expect(readInitialPreference({ injected: 'system', saved: true, legacy: 'dark' })).toBe('system')
    expect(readInitialPreference({ injected: 'invalid', saved: true })).toBe('system')
  })
  it('falls back to the old preference only when no stable preference is saved', () => {
    expect(readInitialPreference({ injected: 'system', saved: false, legacy: 'dark' })).toBe('dark')
    expect(readInitialPreference({ injected: 'system', saved: false, legacy: 'invalid' })).toBe('system')
    expect(readInitialPreference({ injected: 'system', saved: false, legacy: null })).toBe('system')
  })
  it('follows media changes only for system preference', () => {
    const system = ['light', 'dark'].map((_, index) => resolveTheme('system', index === 1))
    const explicitLight = ['light', 'dark'].map((_, index) => resolveTheme('light', index === 1))
    const explicitDark = ['light', 'dark'].map((_, index) => resolveTheme('dark', index === 1))
    expect(system).toEqual(['light', 'dark'])
    expect(explicitLight).toEqual(['light', 'light'])
    expect(explicitDark).toEqual(['dark', 'dark'])
  })
  it('does not let the startup GET overwrite a selection made this session', () => {
    expect(shouldApplyRemoteTheme(false, 0)).toBe(true)
    expect(shouldApplyRemoteTheme(true, 0)).toBe(false)
    expect(shouldApplyRemoteTheme(false, 1)).toBe(false)
  })
})
