import { useEffect, useState, useSyncExternalStore } from 'react'
import { canGoBack, canGoForward, subscribe } from '../shared/browser/historyStack'

export function useHistoryPosition() {
  const back = useSyncExternalStore(subscribe, canGoBack)
  const forward = useSyncExternalStore(subscribe, canGoForward)
  return { back, forward }
}

const editable = (target: EventTarget | null) => target instanceof HTMLElement &&
  (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))

// A horizontal swipe should not leave the page when it can still scroll an
// element sideways, such as a filter rail or a code block.
function scrollsSideways(target: EventTarget | null, delta: number) {
  for (let node = target instanceof Element ? target : null; node && node !== document.body; node = node.parentElement) {
    if (node.scrollWidth <= node.clientWidth + 1) continue
    const overflow = getComputedStyle(node).overflowX
    if (overflow !== 'auto' && overflow !== 'scroll') continue
    if (delta < 0 ? node.scrollLeft > 0 : node.scrollLeft + node.clientWidth < node.scrollWidth - 1) return true
  }
  return false
}

const swipeDistance = 140
const touchEdge = 28

// Back and forward like a browser: mouse side buttons, ⌘[ / ⌘], Alt+←/→,
// a two-finger trackpad swipe and an edge swipe on touch screens. Returns the
// swipe in progress (-1 back, 1 forward, 0 none) to draw a hint.
export function useHistoryGestures() {
  const [hint, setHint] = useState<-1 | 0 | 1>(0)
  useEffect(() => {
    const go = (direction: -1 | 1) => {
      if (direction < 0 ? canGoBack() : canGoForward()) history.go(direction)
    }
    const key = (event: KeyboardEvent) => {
      if (event.defaultPrevented || editable(event.target)) return
      const command = (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey
      const direction = command && event.key === '[' || event.altKey && !command && event.key === 'ArrowLeft' ? -1
        : command && event.key === ']' || event.altKey && !command && event.key === 'ArrowRight' ? 1 : 0
      if (!direction) return
      event.preventDefault()
      go(direction)
    }
    const mouse = (event: MouseEvent) => {
      if (event.button !== 3 && event.button !== 4) return
      event.preventDefault()
      go(event.button === 3 ? -1 : 1)
    }

    // Trackpads report a swipe as a burst of wheel events; a pause ends it.
    let travelled = 0, done = false, timer = 0
    const reset = () => { travelled = 0; done = false; setHint(0) }
    const wheel = (event: WheelEvent) => {
      window.clearTimeout(timer)
      timer = window.setTimeout(reset, 180)
      if (done || event.ctrlKey || Math.abs(event.deltaX) <= Math.abs(event.deltaY) * 1.5) return
      if (scrollsSideways(event.target, event.deltaX)) { travelled = 0; return }
      travelled += event.deltaX
      const direction = travelled < 0 ? -1 : 1
      if (!(direction < 0 ? canGoBack() : canGoForward())) { travelled = 0; return }
      setHint(Math.abs(travelled) > swipeDistance / 3 ? direction : 0)
      if (Math.abs(travelled) < swipeDistance) return
      done = true
      setHint(0)
      go(direction)
    }

    let start: { x: number; y: number; direction: -1 | 1 } | null = null
    const touchStart = (event: TouchEvent) => {
      const touch = event.touches[0]
      start = event.touches.length === 1 && (touch.clientX < touchEdge || touch.clientX > innerWidth - touchEdge)
        ? { x: touch.clientX, y: touch.clientY, direction: touch.clientX < touchEdge ? -1 : 1 } : null
    }
    const touchEnd = (event: TouchEvent) => {
      const touch = event.changedTouches[0]
      if (!start || !touch) return
      const dx = touch.clientX - start.x, dy = touch.clientY - start.y
      if (Math.abs(dx) > 80 && Math.abs(dx) > Math.abs(dy) * 2 && Math.sign(dx) === -start.direction) go(start.direction)
      start = null
    }

    window.addEventListener('keydown', key)
    window.addEventListener('mouseup', mouse)
    window.addEventListener('wheel', wheel, { passive: true })
    window.addEventListener('touchstart', touchStart, { passive: true })
    window.addEventListener('touchend', touchEnd, { passive: true })
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('keydown', key)
      window.removeEventListener('mouseup', mouse)
      window.removeEventListener('wheel', wheel)
      window.removeEventListener('touchstart', touchStart)
      window.removeEventListener('touchend', touchEnd)
    }
  }, [])
  return hint
}
