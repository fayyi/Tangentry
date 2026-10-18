import { afterEach, describe, expect, it, vi } from 'vitest'
import { bindInterfacePreferenceLifecycle, flushInterfacePreferences, resetInterfacePreferenceQueue, setInterfacePreference } from './interfacePreferences'

function response(value: string) {
  return Promise.resolve(new Response(JSON.stringify({ key: 'theme', value, saved: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
}

describe('interface preference writes', () => {
  afterEach(() => { vi.unstubAllGlobals(); resetInterfacePreferenceQueue() })

  it('sends a newer revision when values change quickly', async () => {
    const bodies: { value: string; revision: number }[] = []
    vi.stubGlobal('fetch', vi.fn((_url: string, options: RequestInit) => {
      const body = JSON.parse(String(options.body)) as { value: string; revision: number }
      bodies.push(body)
      return response(body.value)
    }))
    await Promise.all([setInterfacePreference('theme', 'dark'), setInterfacePreference('theme', 'light')])
    expect(bodies[1].revision).toBeGreaterThan(bodies[0].revision)
  })

  it('flushes the latest value while an earlier request is in flight', async () => {
    let resolveFirst: (() => void) | undefined
    const bodies: { value: string; revision: number }[] = []
    vi.stubGlobal('fetch', vi.fn((_url: string, options: RequestInit) => {
      const body = JSON.parse(String(options.body)) as { value: string; revision: number }
      bodies.push(body)
      if (bodies.length === 1) return new Promise(resolve => { resolveFirst = () => resolve(response(body.value)) })
      return response(body.value)
    }))
    void setInterfacePreference('theme', 'dark')
    void setInterfacePreference('theme', 'light')
    flushInterfacePreferences()
    await vi.waitFor(() => expect(bodies).toHaveLength(3))
    expect(bodies[2].value).toBe('light')
    expect(bodies[2].revision).toBeGreaterThan(bodies[0].revision)
    resolveFirst?.()
  })

  it('flushes pending writes when the page becomes hidden', async () => {
    const listeners = new Map<string, () => void>()
    const target = {
      addEventListener: (event: string, callback: EventListenerOrEventListenerObject) => listeners.set(event, callback as () => void),
      removeEventListener: (event: string) => { listeners.delete(event) },
    }
    const fetchMock = vi.fn((_url: string, options: RequestInit) => response(JSON.parse(String(options.body)).value))
    vi.stubGlobal('fetch', fetchMock)
    const unbind = bindInterfacePreferenceLifecycle(target, () => 'hidden')
    void setInterfacePreference('theme', 'dark')
    listeners.get('visibilitychange')?.()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(fetchMock.mock.calls.at(-1)?.[1]).toMatchObject({ keepalive: true })
    unbind()
  })
})
