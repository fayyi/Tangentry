// The app keeps one browser history entry per page it shows. Each entry
// carries its position so the back and forward controls know whether there is
// somewhere to go; the browser does not expose that itself.
type Listener = () => void
const listeners = new Set<Listener>()
const topKey = 'tangentry.history-top'
const position = (state: unknown) => typeof (state as { idx?: unknown } | null)?.idx === 'number' ? (state as { idx: number }).idx : null

let index = position(history.state) ?? 0
let top = Math.max(index, Number(sessionStorage.getItem(topKey)) || 0)
if (position(history.state) === null) history.replaceState({ ...history.state, idx: index }, '')

function publish() {
  try { sessionStorage.setItem(topKey, String(top)) } catch { /* Controls still work for this page load. */ }
  listeners.forEach(listener => listener())
}

export function pushPath(path: string) {
  index++
  top = index
  history.pushState({ idx: index }, '', path)
  publish()
}

// replacePath changes the address of the current entry and keeps its position.
export function replacePath(path: string) {
  history.replaceState({ ...history.state, idx: index }, '', path)
}

// popped must be called from the popstate listener.
export function popped(state: unknown) {
  const next = position(state)
  if (next === null) {
    // An entry the app did not create, such as a hash change: adopt it.
    index++
    top = Math.max(top, index)
    history.replaceState({ ...state as object, idx: index }, '')
  } else index = next
  publish()
}

export const canGoBack = () => index > 0
export const canGoForward = () => index < top
export function subscribe(listener: Listener) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
