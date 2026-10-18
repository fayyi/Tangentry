import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { flushSync } from 'react-dom'
import { getInterfacePreference, setInterfacePreference } from '../shared/preferences/interfacePreferences'

export type ThemePreference = 'light' | 'dark' | 'system'
export const THEME_PREFERENCES: readonly ThemePreference[] = ['light', 'dark', 'system']

export function parseThemePreference(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

export function resolveTheme(preference: ThemePreference, systemDark: boolean): 'light' | 'dark' {
  return preference === 'system' ? (systemDark ? 'dark' : 'light') : preference
}

export function readLegacyPreference(storage?: Pick<Storage, 'getItem'>): ThemePreference | null {
  try {
    const value = (storage ?? localStorage).getItem('tangentry-theme')
    return value === 'dark' || value === 'light' || value === 'system' ? value : null
  } catch {
    return null
  }
}

export function readInitialPreference(input: { injected?: unknown; saved?: boolean; legacy?: unknown; datasetTheme?: string } = {}): ThemePreference {
  const state = (typeof window === 'undefined' ? {} : window) as { __TANGENTRY_THEME__?: unknown; __TANGENTRY_THEME_SAVED__?: boolean }
  const injected = input.injected === undefined ? state.__TANGENTRY_THEME__ : input.injected
  const saved = input.saved === undefined ? state.__TANGENTRY_THEME_SAVED__ : input.saved
  const legacy = input.legacy === undefined
    ? readLegacyPreference()
    : input.legacy === 'light' || input.legacy === 'dark' || input.legacy === 'system' ? input.legacy : null
  if (injected !== undefined && injected !== '__TANGENTRY_THEME__') {
    if (saved) return parseThemePreference(injected)
    return legacy ?? parseThemePreference(injected)
  }
  const datasetTheme = input.datasetTheme === undefined
    ? (typeof document === 'undefined' ? undefined : document.documentElement.dataset.theme)
    : input.datasetTheme
  return legacy ?? parseThemePreference(datasetTheme === 'dark' ? 'dark' : 'system')
}

function systemIsDark() {
  try { return matchMedia('(prefers-color-scheme: dark)').matches } catch { return false }
}

export function subscribeSystemThemeChanges(media: Pick<MediaQueryList, 'addEventListener' | 'removeEventListener'>, onChange: () => void) {
  media.addEventListener?.('change', onChange)
  return () => media.removeEventListener?.('change', onChange)
}

export function useThemeSwitch() {
  const [preference, setPreference] = useState<ThemePreference>(readInitialPreference)
  const [dark, setDark] = useState<boolean>(() => resolveTheme(readInitialPreference(), systemIsDark()) === 'dark')
  const [hydrated, setHydrated] = useState(false)
  const userSelected = useRef(false)
  const intendedDark = useRef(dark)
  const activeTransition = useRef<ViewTransition | null>(null)

  useEffect(() => {
    const apply = () => {
      const nextDark = resolveTheme(preference, systemIsDark()) === 'dark'
      intendedDark.current = nextDark
      setDark(nextDark)
      document.documentElement.dataset.theme = nextDark ? 'dark' : 'light'
    }
    apply()
    const media = matchMedia('(prefers-color-scheme: dark)')
    return subscribeSystemThemeChanges(media, apply)
  }, [preference])

  useEffect(() => {
    if (!hydrated) return
    try { localStorage.setItem('tangentry-theme', preference) } catch { /* Browser fallback is best effort. */ }
    void setInterfacePreference('theme', preference).catch(() => {})
  }, [preference, hydrated])

  useEffect(() => {
    let cancelled = false
    void getInterfacePreference('theme').then(result => {
      if (cancelled) return
      if (result && THEME_PREFERENCES.includes(result.value as ThemePreference) && !userSelected.current) {
        if (result.saved !== false) setPreference(result.value as ThemePreference)
        else {
          const legacy = readLegacyPreference()
          if (legacy) setPreference(legacy)
        }
      }
      setHydrated(true)
    }).catch(() => { setHydrated(true) })
    return () => { cancelled = true }
  }, [])

  useEffect(() => () => { activeTransition.current?.skipTransition() }, [])

  const setTheme = (next: ThemePreference) => {
    userSelected.current = true
    setPreference(parseThemePreference(next))
  }
  const switchTheme = (event: MouseEvent<HTMLButtonElement>) => {
    const next = intendedDark.current ? 'light' : 'dark'
    const apply = () => flushSync(() => {
      intendedDark.current = next === 'dark'
      document.documentElement.dataset.theme = next
      setDark(intendedDark.current)
      setTheme(next)
    })
    if (activeTransition.current) {
      activeTransition.current.skipTransition()
      apply()
      return
    }
    if (!document.startViewTransition || event.detail === 0 || matchMedia('(prefers-reduced-motion: reduce)').matches) {
      apply()
      return
    }
    const rect = event.currentTarget.getBoundingClientRect()
    const x = rect.left + rect.width / 2
    const y = rect.top + rect.height / 2
    const root = document.documentElement
    root.style.setProperty('--theme-origin-x', `${x}px`)
    root.style.setProperty('--theme-origin-y', `${y}px`)
    root.style.setProperty('--theme-radius', `${Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y))}px`)
    root.dataset.themeTransition = 'active'
    const transition = document.startViewTransition(apply)
    activeTransition.current = transition
    void transition.finished.finally(() => {
      if (activeTransition.current !== transition) return
      activeTransition.current = null
      delete root.dataset.themeTransition
      for (const property of ['--theme-origin-x', '--theme-origin-y', '--theme-radius']) root.style.removeProperty(property)
    }).catch(() => {})
  }
  return { dark, preference, setTheme, switchTheme }
}
