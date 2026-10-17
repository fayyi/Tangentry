import { describe, expect, it } from 'vitest'
import { showsManualLogin, supportsInAppLogin, usesAdvancedManualLogin } from './platformLoginSettings'

describe('platform login settings', () => {
  it('only enables embedded login for platforms with a native flow', () => {
    expect(supportsInAppLogin('bilibili')).toBe(true)
    expect(supportsInAppLogin('xiaohongshu')).toBe(true)
    expect(supportsInAppLogin('youtube')).toBe(true)
    expect(supportsInAppLogin('xiaoyuzhou')).toBe(false)
  })

  it('keeps manual entry for server deployments and YouTube fallback', () => {
    expect(showsManualLogin('bilibili', false)).toBe(true)
    expect(showsManualLogin('bilibili', true)).toBe(false)
    expect(showsManualLogin('youtube', true)).toBe(true)
  })

  it('marks target-platform manual settings as advanced on servers', () => {
    expect(usesAdvancedManualLogin('bilibili', false)).toBe(true)
    expect(usesAdvancedManualLogin('xiaoyuzhou', false)).toBe(true)
    expect(usesAdvancedManualLogin('linuxdo', false)).toBe(false)
    expect(usesAdvancedManualLogin('bilibili', true)).toBe(false)
  })
})
