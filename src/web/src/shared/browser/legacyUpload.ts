import { APIError } from '../api/client'

const running = new Map<string, Promise<void>>()

// Moves records kept in browser storage by earlier versions into the local
// database, once per browser. The data is cleared only after the server has
// taken it (or rejected it as invalid), so a failed upload retries next load;
// the marker stops later writes to the old key from being imported again.
export function uploadLegacyRecords<T>(key: string, read: () => T[], upload: (values: T[]) => Promise<unknown>): Promise<void> {
  const marker = `${key}.migrated`
  const existing = running.get(key)
  if (existing) return existing
  const migrate = async () => {
    try {
      if (localStorage.getItem(marker)) return
      const values = read()
      if (values.length) {
        try { await upload(values) }
        catch (error) { if (!(error instanceof APIError && error.status === 400)) throw error }
      }
      localStorage.removeItem(key)
      localStorage.setItem(marker, '1')
    } catch { /* storage or network unavailable: retry on the next load */ }
  }
  const task = (navigator.locks ? navigator.locks.request(key, migrate) : migrate()).finally(() => running.delete(key))
  running.set(key, task)
  return task
}
