// Dates follow the viewer's local timezone, including daylight-saving boundaries.
export function localDay(timestamp: number): string {
  const day = new Date(timestamp * 1000)
  return `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, '0')}-${String(day.getDate()).padStart(2, '0')}`
}
export function dayLabel(timestamp: number): string {
  const date = localDay(timestamp), today = new Date(), yesterday = new Date()
  yesterday.setDate(yesterday.getDate() - 1)
  if (date === localDay(today.getTime() / 1000)) return '今天'
  if (date === localDay(yesterday.getTime() / 1000)) return '昨天'
  return new Date(timestamp * 1000).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })
}
export function dateRange(from: string, to: string): { from: number; to: number } | null {
  const parse = (raw: string, exclusiveEnd: boolean): number | null => {
    if (!raw) return 0
    if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return null
    const [year, month, day] = raw.split('-').map(Number)
    if (year < 1970 || year > 9998) return null
    const date = new Date(year, month - 1, day)
    if (date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day) return null
    if (exclusiveEnd) date.setDate(date.getDate() + 1)
    return Math.floor(date.getTime() / 1000)
  }
  const start = parse(from, false), end = parse(to, true)
  return start === null || end === null || (end !== 0 && end <= start) ? null : { from: start, to: end }
}
