import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  bindInterfacePreferenceLifecycle,
  flushInterfacePreferences,
  getPendingInterfacePreference,
  resetInterfacePreferenceQueue,
  setInterfacePreference,
} from './interfacePreferences'

function response(value: string) {
  return Promise.resolve(new Response(JSON.stringify({ key: 'theme', value, saved: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

describe('interface preference writes', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    resetInterfacePreferenceQueue()
  })

  it('serializes rapid writes and keeps the last value', async () => {
    const calls: { value: string; resolve: () => void }[] = []
    vi.stubGlobal('fetch', vi.fn((_url: string, options: RequestInit) => {
      const value = JSON.parse(String(options.body)).value as string
      return new Promise(resolve => calls.push({ value, resolve: () => resolve(response(value)) }))
    }))
    const first = setInterfacePreference('theme', 'dark')
    const second = setInterfacePreference('theme', 'light')
    expect(getPendingInterfacePreference('theme')).toBe('light')
    expect(calls.map(call => call.value)).toEqual(['dark'])
    calls[0].resolve()
    await first
    expect(calls.map(call => call.value)).toEqual(['dark', 'light'])
    calls[1].resolve()
    await second
    expect(calls.map(call => call.value)).toEqual(['dark', 'light'])
  })

  it('retains a failed write and retries on flush', async () => {
    let attempts = 0
    vi.stubGlobal('fetch', vi.fn((_url: string, options: RequestInit) => {
      attempts++
      if (attempts === 1) return Promise.reject(new Error('offline'))
      return response(JSON.parse(String(options.body)).value)
    }))
    await expect(setInterfacePreference('theme', 'dark')).rejects.toThrow('无法连接 Tangentry')
    expect(attempts).toBe(1)
    flushInterfacePreferences()
    await vi.waitFor(() => expect(attempts).toBe(2))
  })

  it('flushes queued writes when the page becomes hidden', async () => {
    const listeners = new Map<string, () => void>()
    const target = {
      addEventListener: (event: string, callback: EventListenerOrEventListenerObject) => listeners.set(event, callback as () => void),
      removeEventListener: (event: string) => { listeners.delete(event) },
    }
    let attempts = 0
    const fetchMock = vi.fn((_url: string, options: RequestInit) => {
      attempts++
      if (attempts === 1) return Promise.reject(new Error('offline'))
      return response(JSON.parse(String(options.body)).value)
    })
    vi.stubGlobal('fetch', fetchMock)
    const unbind = bindInterfacePreferenceLifecycle(target, () => 'hidden')
    const pending = setInterfacePreference('theme', 'dark')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await expect(pending).rejects.toThrow('无法连接 Tangentry')
    listeners.get('visibilitychange')?.()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ keepalive: true })
    unbind()
  })
})
