const listeners = new Set<(contentId: string) => void>()
export function notifyNotesChanged(contentId: string) { for (const listener of listeners) listener(contentId) }
export function subscribeNotesChanged(listener: (contentId: string) => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
