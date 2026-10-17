// Limit platform requests without dropping a source during an all-source refresh.
export function recommendationRequests(limit = 3) {
  let active = 0
  const waiting: (() => void)[] = []
  return async function acquire(signal: AbortSignal): Promise<() => void> {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    await new Promise<void>((resolve, reject) => {
      const start = () => {
        signal.removeEventListener('abort', cancel)
        active++; resolve()
      }
      const cancel = () => {
        const index = waiting.indexOf(start)
        if (index >= 0) waiting.splice(index, 1)
        reject(new DOMException('Aborted', 'AbortError'))
      }
      if (active < limit) start()
      else { waiting.push(start); signal.addEventListener('abort', cancel, { once: true }) }
    })
    let released = false
    return () => {
      if (released) return
      released = true; active--
      waiting.shift()?.()
    }
  }
}
