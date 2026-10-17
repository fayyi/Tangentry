import type { ReactNode } from 'react'

export function timestamp(seconds: number): string {
  const total = Math.round(seconds * 1000)
  const hours = Math.floor(total / 3600000)
  const minutes = Math.floor(total / 60000) % 60
  const secs = Math.floor(total / 1000) % 60
  const clock = `${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
  return hours > 0 ? `${hours}:${clock}` : clock
}

export function highlightMatch(value: string, query: string): ReactNode {
  if (!query.trim()) return value
  const needle = query.trim()
  const matches = value.matchAll(new RegExp(needle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'giu'))
  const parts: ReactNode[] = []
  let cursor = 0
  for (const match of matches) {
    const index = match.index
    if (index > cursor) parts.push(value.slice(cursor, index))
    parts.push(<mark key={index} className="subtitle-match">{match[0]}</mark>)
    cursor = index + match[0].length
  }
  if (cursor < value.length) parts.push(value.slice(cursor))
  return parts
}
