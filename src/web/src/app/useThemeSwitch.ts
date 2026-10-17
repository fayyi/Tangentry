import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { flushSync } from 'react-dom'

export function useThemeSwitch() {
  const [dark, setDark] = useState(() => {
    try { return localStorage.getItem('tangentry-theme') === 'dark' } catch { return false }
  })
  const intendedDark = useRef(dark)
  const activeTransition = useRef<ViewTransition | null>(null)
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    try { localStorage.setItem('tangentry-theme', dark ? 'dark' : 'light') } catch { /* Keep the session theme when storage is unavailable. */ }
  }, [dark])
  useEffect(() => () => { activeTransition.current?.skipTransition() }, [])

  const switchTheme = (event: MouseEvent<HTMLButtonElement>) => {
    intendedDark.current = !intendedDark.current
    // Persist the choice before a deferred transition can be interrupted by reload.
    try { localStorage.setItem('tangentry-theme', intendedDark.current ? 'dark' : 'light') } catch { /* Keep the session theme when storage is unavailable. */ }
    // A skipped transition may still run its update callback; use the latest intent.
    const apply = () => flushSync(() => {
      document.documentElement.dataset.theme = intendedDark.current ? 'dark' : 'light'
      setDark(intendedDark.current)
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
  return { dark, switchTheme }
}
