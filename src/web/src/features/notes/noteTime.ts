export function formatPosition(value: number): string {
  const total = Math.max(0, Math.floor(value))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60) % 60
  const seconds = total % 60
  return `${hours ? `${hours}:` : ''}${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

export function validPosition(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 7 * 24 * 3600 ? value : null
}

export function capturePosition(value: unknown): number | null {
  const position = validPosition(value)
  return position === null ? null : Math.floor(position)
}
