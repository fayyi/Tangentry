import type { SavedViewFilters } from '../../shared/api/types'

export const emptySavedViewFilters: SavedViewFilters = {query:'',sourceId:'',groupId:'',platform:'',kind:'',topic:'',tag:'',read:'all',favorite:false,sinceOffset:'',since:0,until:0}

export function savedViewDefinition(filters: SavedViewFilters) {
  return { version: 1, filters: { ...emptySavedViewFilters, ...filters } }
}

export function savedViewPath(id: string) {
  return `/subscriptions?view=${encodeURIComponent(id)}`
}

export function dateFilterTimestamp(value: string, end = false) {
  if (!value) return 0
  const date = new Date(`${value}T${end ? '23:59:59' : '00:00:00'}`)
  return Number.isNaN(date.getTime()) ? 0 : Math.floor(date.getTime() / 1000)
}

export function relativeDateFilterTimestamp(value: string, now = new Date()) {
  const day = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  if (value === 'day') return Math.floor(day.getTime() / 1000)
  if (value === 'week') {
    const monday = new Date(day)
    monday.setDate(day.getDate() - ((day.getDay() + 6) % 7))
    return Math.floor(monday.getTime() / 1000)
  }
  if (value === 'month') return Math.floor(new Date(now.getFullYear(), now.getMonth(), 1).getTime() / 1000)
  return 0
}

export function localDateInputValue(seconds: number) {
  if (!seconds) return ''
  const date = new Date(seconds * 1000)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
