export const READER_LAYOUT_KEY = 'tangentry.reader-layout.v1'
export const DEFAULT_READER_LAYOUT = { ai: 280, notes: 420, player: 100 }
export const READER_LAYOUT_LIMITS = { ai: [220, 420] as const, notes: [300, 560] as const, player: [45, 100] as const }

export function clampLayoutWidth(value: number, limits: readonly [number, number]) {
  return Math.min(limits[1], Math.max(limits[0], Number.isFinite(value) ? value : limits[0]))
}

export type ReaderLayout = typeof DEFAULT_READER_LAYOUT

export function readReaderLayout(storage: Pick<Storage, 'getItem'> = localStorage): ReaderLayout {
  try {
    const raw = JSON.parse(storage.getItem(READER_LAYOUT_KEY) ?? 'null') as Partial<ReaderLayout> | null
    return {
      ai: clampLayoutWidth(raw?.ai ?? DEFAULT_READER_LAYOUT.ai, READER_LAYOUT_LIMITS.ai),
      notes: clampLayoutWidth(raw?.notes ?? DEFAULT_READER_LAYOUT.notes, READER_LAYOUT_LIMITS.notes),
      player: clampLayoutWidth(raw?.player ?? DEFAULT_READER_LAYOUT.player, READER_LAYOUT_LIMITS.player),
    }
  } catch {
    return { ...DEFAULT_READER_LAYOUT }
  }
}

export function writeReaderLayout(value: ReaderLayout, storage: Pick<Storage, 'setItem'> = localStorage) {
  try { storage.setItem(READER_LAYOUT_KEY, JSON.stringify(value)) } catch { /* Layout remains usable without optional storage. */ }
}

export function adjustLayoutWidth(value: number, delta: number, limits: readonly [number, number]) {
  return clampLayoutWidth(value + delta, limits)
}
