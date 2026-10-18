import { request } from '../api/client'

export type InterfacePreferenceKey = 'theme'

export async function getInterfacePreference(key: InterfacePreferenceKey) {
  return request<{ key: InterfacePreferenceKey; value: string; saved?: boolean }>(`/preferences/${encodeURIComponent(key)}`)
}

export async function setInterfacePreference(key: InterfacePreferenceKey, value: string) {
  return request<{ key: InterfacePreferenceKey; value: string; saved: boolean }>(`/preferences/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
  })
}
