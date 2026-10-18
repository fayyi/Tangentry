import { useCallback, useEffect, useState } from 'react'

export type UIFont = 'system' | 'inter' | 'noto'
export type ReadingFont = 'system' | 'wenkai' | 'noto'
export type ReadingSize = 'small' | 'medium' | 'large'
export type ReadingLineHeight = 'compact' | 'comfortable' | 'relaxed'

export interface FontSettings {
  ui: UIFont
  reading: ReadingFont
  size: ReadingSize
  lineHeight: ReadingLineHeight
}

export const defaultFontSettings: FontSettings = {
  ui: 'inter',
  reading: 'wenkai',
  size: 'medium',
  lineHeight: 'comfortable',
}

export const fontSettingsKey = 'tangentry-font-settings'
export const fontSettingsChangedEvent = 'tangentry-font-settings-changed'

export const uiFontStacks: Record<UIFont, string> = {
  system: "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Segoe UI', 'Microsoft YaHei', 'Noto Sans SC', sans-serif",
  inter: "'Inter', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Segoe UI', 'Microsoft YaHei', 'Noto Sans SC', sans-serif",
  noto: "'Noto Sans SC', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Segoe UI', 'Microsoft YaHei', sans-serif",
}

export const readingFontStacks: Record<ReadingFont, string> = {
  system: "'Inter', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Segoe UI', 'Microsoft YaHei', 'Noto Sans SC', sans-serif",
  wenkai: "'Inter', 'LXGW WenKai Screen', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', 'Noto Sans SC', sans-serif",
  noto: "'Inter', 'Noto Sans SC', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif",
}

export const readingSizes: Record<ReadingSize, string> = { small: '14px', medium: '15px', large: '17px' }
export const readingLineHeights: Record<ReadingLineHeight, string> = { compact: '1.7', comfortable: '1.9', relaxed: '2.1' }

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>

function option<T extends string>(value: unknown, values: Record<T, string>, fallback: T): T {
  return typeof value === 'string' && value in values ? value as T : fallback
}

function availableStorage(): StorageLike | undefined {
  try { return typeof localStorage === 'undefined' ? undefined : localStorage } catch { return undefined }
}

export function validateFontSettings(value: unknown): FontSettings {
  const raw = (value && typeof value === 'object' ? value : {}) as Partial<FontSettings>
  return {
    ui: option(raw.ui, uiFontStacks, defaultFontSettings.ui),
    reading: option(raw.reading, readingFontStacks, defaultFontSettings.reading),
    size: option(raw.size, readingSizes, defaultFontSettings.size),
    lineHeight: option(raw.lineHeight, readingLineHeights, defaultFontSettings.lineHeight),
  }
}

export function readFontSettings(storage: StorageLike | undefined = availableStorage()): FontSettings {
  try {
    return validateFontSettings(JSON.parse(storage?.getItem(fontSettingsKey) ?? 'null'))
  } catch {
    return { ...defaultFontSettings }
  }
}

export function writeFontSettings(value: FontSettings, storage: StorageLike | undefined = availableStorage()): FontSettings {
  const next = validateFontSettings(value)
  try { storage?.setItem(fontSettingsKey, JSON.stringify(next)) } catch { /* Apply the preference for this session. */ }
  return next
}

export function applyFontSettings(value: FontSettings, root: HTMLElement = document.documentElement) {
  const settings = validateFontSettings(value)
  root.style.setProperty('--font-ui', uiFontStacks[settings.ui])
  root.style.setProperty('--font-display', uiFontStacks[settings.ui])
  root.style.setProperty('--font-reading', readingFontStacks[settings.reading])
  root.style.setProperty('--font-reading-size', readingSizes[settings.size])
  root.style.setProperty('--font-reading-line-height', readingLineHeights[settings.lineHeight])
  root.dataset.fontUi = settings.ui
  root.dataset.fontReading = settings.reading
  root.dataset.fontReadingSize = settings.size
  root.dataset.fontReadingLineHeight = settings.lineHeight
}

export function useFontSettings() {
  const [settings, setSettings] = useState(readFontSettings)

  useEffect(() => { applyFontSettings(settings) }, [settings])
  useEffect(() => {
    const storage = (event: StorageEvent) => { if (event.key === fontSettingsKey || event.key === null) setSettings(readFontSettings()) }
    const local = (event: Event) => setSettings(validateFontSettings((event as CustomEvent<FontSettings>).detail))
    window.addEventListener('storage', storage)
    window.addEventListener(fontSettingsChangedEvent, local)
    return () => {
      window.removeEventListener('storage', storage)
      window.removeEventListener(fontSettingsChangedEvent, local)
    }
  }, [])

  const change = useCallback((patch: Partial<FontSettings>) => {
    setSettings(current => {
      const next = writeFontSettings({ ...current, ...patch })
      window.dispatchEvent(new CustomEvent(fontSettingsChangedEvent, { detail: next }))
      return next
    })
  }, [])
  const reset = useCallback(() => change(defaultFontSettings), [change])
  return { settings, change, reset }
}
