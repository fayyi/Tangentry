import { useCallback, useEffect, useState } from 'react'

export type CardDensity = 'compact' | 'comfortable' | 'spacious'
export type CardTitleSize = 'small' | 'medium' | 'large'
export type CardCorners = 'square' | 'soft' | 'round'
export interface CardAppearance { width: number; density: CardDensity; titleSize: CardTitleSize; corners: CardCorners; excerpt: boolean }

export const cardWidthRange = { min: 180, max: 420, step: 10 } as const
export const defaultCardAppearance: CardAppearance = { width: 248, density: 'comfortable', titleSize: 'medium', corners: 'soft', excerpt: true }
const key = 'tangentry.card-appearance.v1'
const changed = 'tangentry-card-appearance-changed'
const densities: Record<CardDensity, { gap: string; pad: string }> = { compact: { gap: '10px', pad: '9px 11px' }, comfortable: { gap: '18px', pad: '12px 14px' }, spacious: { gap: '26px', pad: '16px 18px' } }
const titles: Record<CardTitleSize, string> = { small: '13px', medium: '14px', large: '15.5px' }
const corners: Record<CardCorners, string> = { square: '3px', soft: '12px', round: '20px' }

function valid(value: unknown): CardAppearance {
  const raw = (value && typeof value === 'object' ? value : {}) as Partial<CardAppearance>
  const width = typeof raw.width === 'number' && Number.isFinite(raw.width) ? Math.min(cardWidthRange.max, Math.max(cardWidthRange.min, Math.round(raw.width / cardWidthRange.step) * cardWidthRange.step)) : defaultCardAppearance.width
  return {
    width,
    density: raw.density && raw.density in densities ? raw.density : defaultCardAppearance.density,
    titleSize: raw.titleSize && raw.titleSize in titles ? raw.titleSize : defaultCardAppearance.titleSize,
    corners: raw.corners && raw.corners in corners ? raw.corners : defaultCardAppearance.corners,
    excerpt: typeof raw.excerpt === 'boolean' ? raw.excerpt : defaultCardAppearance.excerpt,
  }
}
function read(): CardAppearance {
  try { return valid(JSON.parse(localStorage.getItem(key) ?? 'null')) } catch { return defaultCardAppearance }
}
export function applyCardAppearance(value: CardAppearance, root: HTMLElement = document.documentElement) {
  root.style.setProperty('--card-min', `${value.width}px`)
  root.style.setProperty('--card-gap', densities[value.density].gap)
  root.style.setProperty('--card-content-pad', densities[value.density].pad)
  root.style.setProperty('--card-title-size', titles[value.titleSize])
  root.style.setProperty('--card-radius', corners[value.corners])
  root.dataset.cardExcerpt = value.excerpt ? 'on' : 'off'
}

// Card appearance is a per-device reading preference, applied as CSS variables.
export function useCardAppearance() {
  const [appearance, setAppearance] = useState(read)
  useEffect(() => { applyCardAppearance(appearance) }, [appearance])
  useEffect(() => {
    const storage = (event: StorageEvent) => { if (event.key === key || event.key === null) setAppearance(read()) }
    const local = (event: Event) => setAppearance((event as CustomEvent<CardAppearance>).detail)
    window.addEventListener('storage', storage)
    window.addEventListener(changed, local)
    return () => { window.removeEventListener('storage', storage); window.removeEventListener(changed, local) }
  }, [])
  const change = useCallback((patch: Partial<CardAppearance>) => {
    setAppearance(current => {
      const next = valid({ ...current, ...patch })
      try { localStorage.setItem(key, JSON.stringify(next)) } catch { /* Applies for this session. */ }
      window.dispatchEvent(new CustomEvent(changed, { detail: next }))
      return next
    })
  }, [])
  const reset = useCallback(() => change(defaultCardAppearance), [change])
  return { appearance, change, reset }
}
