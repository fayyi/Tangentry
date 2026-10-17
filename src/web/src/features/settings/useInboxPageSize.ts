import { useEffect, useState } from 'react'

export const inboxPageSizes = [10, 20, 30, 50] as const
const key = 'tangentry-inbox-page-size'
const changed = 'tangentry-inbox-page-size-changed'
const read = () => {
  try {
    const value = Number(localStorage.getItem(key))
    return inboxPageSizes.some(size => size === value) ? value : 50
  } catch { return 50 }
}

export function useInboxPageSize() {
  const [pageSize, setPageSize] = useState(read)
  useEffect(() => {
    const sync = () => setPageSize(read())
    const storage = (event: StorageEvent) => { if (event.key === key || event.key === null) sync() }
    const local = (event: Event) => setPageSize((event as CustomEvent<number>).detail)
    window.addEventListener('storage', storage)
    window.addEventListener(changed, local)
    return () => { window.removeEventListener('storage', storage); window.removeEventListener(changed, local) }
  }, [])
  const changePageSize = (value: number) => {
    if (!inboxPageSizes.some(size => size === value)) return
    try { localStorage.setItem(key, String(value)) } catch { /* Apply the preference for this session. */ }
    setPageSize(value)
    window.dispatchEvent(new CustomEvent(changed, { detail: value }))
  }
  return { pageSize, changePageSize }
}
