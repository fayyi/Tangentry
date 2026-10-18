import { request } from '../api/client'

export type InterfacePreferenceKey = 'theme'

type PendingWrite = { value: string; revision: number }
type Waiter = { revision: number; resolve: () => void; reject: (error: unknown) => void }
type Queue = { revision: number; pending?: PendingWrite; inFlight?: PendingWrite; waiters: Waiter[] }

const queues = new Map<InterfacePreferenceKey, Queue>()
const pendingStorageKey = (key: InterfacePreferenceKey) => `tangentry-interface-preference-pending-${key}`

function persistPending(key: InterfacePreferenceKey, pending: PendingWrite | undefined) {
  try {
    if (pending) localStorage.setItem(pendingStorageKey(key), JSON.stringify(pending))
    else localStorage.removeItem(pendingStorageKey(key))
  } catch {
    // Browser storage is best effort; the in-memory queue still protects this page.
  }
}

function restorePending(key: InterfacePreferenceKey) {
  try {
    const raw = localStorage.getItem(pendingStorageKey(key))
    if (!raw) return
    const value = JSON.parse(raw) as Partial<PendingWrite>
    if (typeof value.value !== 'string' || typeof value.revision !== 'number' || !Number.isSafeInteger(value.revision) || value.revision < 1) return
    const queue = queueFor(key)
    queue.revision = Math.max(queue.revision, value.revision)
    queue.pending = { value: value.value, revision: value.revision }
  } catch {
    // Ignore malformed or unavailable browser storage.
  }
}

export function getPendingInterfacePreference(key: InterfacePreferenceKey): string | null {
  const queue = queues.get(key)
  if (queue?.pending) return queue.pending.value
  if (queue?.inFlight) return queue.inFlight.value
  try {
    const raw = localStorage.getItem(pendingStorageKey(key))
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<PendingWrite>
    return typeof value.value === 'string' ? value.value : null
  } catch {
    return null
  }
}

function queueFor(key: InterfacePreferenceKey) {
  let queue = queues.get(key)
  if (!queue) {
    queue = { revision: 0, waiters: [] }
    queues.set(key, queue)
  }
  return queue
}

function settle(queue: Queue, revision: number, error?: unknown) {
  const remaining: Waiter[] = []
  for (const waiter of queue.waiters) {
    if (waiter.revision <= revision) {
      if (error === undefined) waiter.resolve()
      else waiter.reject(error)
    } else remaining.push(waiter)
  }
  queue.waiters = remaining
}

function drain(key: InterfacePreferenceKey) {
  const queue = queueFor(key)
  if (queue.inFlight || !queue.pending) return
  const write = queue.pending
  queue.pending = undefined
  queue.inFlight = write
  void request<{ key: InterfacePreferenceKey; value: string; saved: boolean; revision?: number }>(`/preferences/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value: write.value, revision: write.revision }),
    keepalive: true,
  }).then(() => {
    if (queue.inFlight?.revision !== write.revision) return
    queue.inFlight = undefined
    if (!queue.pending) persistPending(key, undefined)
    settle(queue, write.revision)
    drain(key)
  }).catch(error => {
    if (queue.inFlight?.revision !== write.revision) return
    queue.inFlight = undefined
    // Keep a failed write for a later visibility/page lifecycle retry unless a
    // newer value is already queued, in which case that value supersedes it.
    if (!queue.pending || queue.pending.revision < write.revision) queue.pending = write
    persistPending(key, queue.pending)
    settle(queue, write.revision, error)
    if (queue.pending && queue.pending.revision > write.revision) drain(key)
  })
}

export async function getInterfacePreference(key: InterfacePreferenceKey) {
  return request<{ key: InterfacePreferenceKey; value: string; saved?: boolean }>(`/preferences/${encodeURIComponent(key)}`)
}

export function setInterfacePreference(key: InterfacePreferenceKey, value: string) {
  const queue = queueFor(key)
  const revision = ++queue.revision
  queue.pending = { value, revision }
  persistPending(key, queue.pending)
  const promise = new Promise<void>((resolve, reject) => queue.waiters.push({ revision, resolve, reject }))
  drain(key)
  return promise
}

export function flushInterfacePreferences() {
  for (const key of queues.keys()) drain(key)
}

type LifecycleTarget = {
  addEventListener: (event: string, callback: EventListenerOrEventListenerObject) => void
  removeEventListener: (event: string, callback: EventListenerOrEventListenerObject) => void
}

export function bindInterfacePreferenceLifecycle(target: LifecycleTarget, visibility: () => DocumentVisibilityState = () => {
  return typeof document === 'undefined' ? 'visible' : document.visibilityState
}, visibilityTarget: LifecycleTarget = target) {
  const flush = () => flushInterfacePreferences()
  const onVisibilityChange = () => {
    if (visibility() === 'hidden' || visibility() === 'visible') flush()
  }
  target.addEventListener('pagehide', flush)
  target.addEventListener('beforeunload', flush)
  visibilityTarget.addEventListener('visibilitychange', onVisibilityChange)
  return () => {
    target.removeEventListener('pagehide', flush)
    target.removeEventListener('beforeunload', flush)
    visibilityTarget.removeEventListener('visibilitychange', onVisibilityChange)
  }
}

if (typeof window !== 'undefined') {
  restorePending('theme')
  bindInterfacePreferenceLifecycle(window, undefined, document)
  flushInterfacePreferences()
}

export function resetInterfacePreferenceQueue() {
  queues.clear()
}
