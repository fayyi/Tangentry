import { useState, type ReactNode } from 'react'

export function SettingsGroup({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  const key = 'tangentry-settings-group-' + id
  const [expanded, setExpanded] = useState(() => {
    try { return localStorage.getItem(key) !== 'closed' } catch { return true }
  })
  return <details className="tools-group" open={expanded} onToggle={event => {
    const open = event.currentTarget.open
    setExpanded(open)
    try { localStorage.setItem(key, open ? 'open' : 'closed') } catch { /* Keep the current session state. */ }
  }}>
    <summary onClick={event => {
      event.preventDefault()
      const open = !expanded
      setExpanded(open)
      try { localStorage.setItem(key, open ? 'open' : 'closed') } catch { /* Keep the current session state. */ }
    }}>{title}</summary>{children}
  </details>
}
