import { useEffect, useState } from 'react'
import { request } from './client'

// Features only the desktop shell provides. The server reports them at
// /api/capabilities; a headless server or an older backend reports none.
export type Capabilities = {
  pathPicker: boolean
  windowCapture: boolean
  notifications: boolean
  keepAwake: boolean
  /** Opens the native, platform-scoped WebView login window. */
  platformWebViewLogin: boolean
}

const none: Capabilities = { pathPicker: false, windowCapture: false, notifications: false, keepAwake: false, platformWebViewLogin: false }
let current: Capabilities | undefined
let loading: Promise<Capabilities> | undefined

export function loadCapabilities() {
  loading ??= request<Partial<Capabilities>>('/capabilities').then(
    result => (current = { ...none, ...result }),
    () => { loading = undefined; return none },
  )
  return loading
}

export function useCapabilities() {
  const [capabilities, setCapabilities] = useState(current ?? none)
  useEffect(() => {
    let active = true
    void loadCapabilities().then(result => { if (active) setCapabilities(result) })
    return () => { active = false }
  }, [])
  return capabilities
}

export async function selectDesktopPath(kind: string, current: string) {
  return request<{ path: string; cancelled: boolean }>('/desktop/select-path', {
    method: 'POST', body: JSON.stringify({ kind, current }),
  })
}

export type PlatformWebView = 'linuxdo' | 'douyin'

/** Ask the desktop shell to open the restricted, platform-specific login window. */
export async function openDesktopPlatformLogin(platform: PlatformWebView) {
  return request<{ started: boolean }>('/desktop/platform-login', {
    method: 'POST',
    body: JSON.stringify({ platform }),
  })
}
