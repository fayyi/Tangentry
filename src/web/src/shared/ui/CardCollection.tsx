import { Children, isValidElement, useLayoutEffect, useRef } from 'react'
import type { HTMLAttributes } from 'react'

type CardSize = { width: number; height: number }

/** Avoid a masonry write when a card's measured box is unchanged. */
export function cardSizeChanged(previous: CardSize | undefined, width: number, height: number) {
  return !previous || previous.width !== width || previous.height !== height
}

export function masonryIntrinsicSize(height: number) {
  return `auto ${Math.max(0, Math.ceil(height))}px`
}

export function masonryNeedsMeasurement(cardCount: number, measuredCount: number, spacingChanged: boolean) {
  return spacingChanged || measuredCount < cardCount
}

type BorderBoxSize = { inlineSize: number; blockSize: number }
type ResizeBoxEntry = { target?: Element; borderBoxSize?: ReadonlyArray<BorderBoxSize> }

/** Read border-box dimensions consistently for initial and observer measurements. */
export function borderBoxDimensions(entry: ResizeBoxEntry, fallback: () => { width: number; height: number }) {
  const border = entry.borderBoxSize?.[0]
  return border ? { width: border.inlineSize, height: border.blockSize } : fallback()
}

export function masonrySpan(height: number, spacing: number) {
  return Math.ceil(height + spacing)
}

export function CardCollection({ children, masonry = true, rowGap = 16, ...props }: HTMLAttributes<HTMLDivElement> & { masonry?: boolean; rowGap?: number }) {
  const collection = useRef<HTMLDivElement>(null)
  const measurements = useRef(new WeakMap<HTMLElement, CardSize>())
  const spacingRef = useRef<number | undefined>(undefined)
  // Re-measure when the set of cards changes, not on every parent render:
  // ResizeObserver already follows each card's own height changes.
  const cards = Children.toArray(children).map(child => isValidElement(child) ? String(child.key) : '').join('\n')
  useLayoutEffect(() => {
    const node = collection.current
    if (!node) return
    const scrollTop = window.scrollY
    const cards = Array.from(node.children) as HTMLElement[]
    if (!masonry) {
      cards.forEach(card => {
        card.style.removeProperty('grid-row-end')
        card.style.removeProperty('content-visibility')
        card.style.removeProperty('contain-intrinsic-size')
      })
      return
    }
    // One-pixel tracks let each card keep its own height and fill earlier gaps.
    // Row spacing follows the user's card spacing relative to the standard 18px column gap.
    const gap = parseFloat(getComputedStyle(node).columnGap)
    const spacing = Number.isFinite(gap) && gap > 0 ? Math.max(4, rowGap + gap - 18) : rowGap
    const spacingChanged = spacingRef.current !== spacing
    spacingRef.current = spacing
    if (spacingChanged) measurements.current = new WeakMap<HTMLElement, CardSize>()
    const hasMeasurements = !spacingChanged && cards.length > 0 && cards.every(card => measurements.current.has(card))
    const measuredCount = cards.filter(card => measurements.current.has(card) && card.style.gridRowEnd).length
    const shouldMeasure = masonryNeedsMeasurement(cards.length, measuredCount, spacingChanged)
    const resize = (entries: ResizeBoxEntry[]) => {
      const updates: Array<{ card: HTMLElement; width: number; height: number }> = []
      for (const entry of entries) {
        const target = entry.target
        const card = target as HTMLElement
        const box = borderBoxDimensions(entry, () => {
          const rect = card.getBoundingClientRect()
          return { width: rect.width, height: rect.height }
        })
        if (cardSizeChanged(measurements.current.get(card), box.width, box.height)) {
          updates.push({ card, width: box.width, height: box.height })
        }
      }
      for (const { card, width, height } of updates) {
        measurements.current.set(card, { width, height })
        card.style.gridRowEnd = `span ${masonrySpan(height, spacing)}`
        if (hasMeasurements) {
          card.style.contentVisibility = 'auto'
          card.style.containIntrinsicSize = masonryIntrinsicSize(height)
        }
      }
    }
    // Activity hides and later reveals the collection by restarting effects while
    // preserving the DOM. Existing spans keep the page geometry stable, so let
    // ResizeObserver refresh them asynchronously instead of synchronously
    // measuring every card during reveal.
    const cardsToMeasure = shouldMeasure
      ? cards.filter(card => spacingChanged || !measurements.current.has(card) || !card.style.gridRowEnd)
      : []
    if (cardsToMeasure.length) resize(cardsToMeasure.map(target => ({ target })))
    if (Math.abs(window.scrollY - scrollTop) > 1) window.scrollTo({ top: scrollTop, behavior: 'auto' })
    const observer = new ResizeObserver(resize)
    const applyIntrinsicStyles = () => {
      if (hasMeasurements || !shouldMeasure) return
      for (const card of cards) {
        const measurement = measurements.current.get(card)
        if (!measurement) continue
        card.style.contentVisibility = 'auto'
        card.style.containIntrinsicSize = masonryIntrinsicSize(measurement.height)
      }
    }
    let cancelObserve: () => void
    if (!hasMeasurements) {
      const idle = 'requestIdleCallback' in window
        ? window.requestIdleCallback(() => { applyIntrinsicStyles(); cards.forEach(card => observer.observe(card)) })
        : setTimeout(() => { applyIntrinsicStyles(); cards.forEach(card => observer.observe(card)) }, 0)
      cancelObserve = () => {
        if ('cancelIdleCallback' in window) window.cancelIdleCallback(idle as number)
        else clearTimeout(idle)
      }
    } else if ('requestIdleCallback' in window) {
      const idle = window.requestIdleCallback(() => cards.forEach(card => observer.observe(card)))
      cancelObserve = () => window.cancelIdleCallback(idle)
    } else {
      const timeout = setTimeout(() => cards.forEach(card => observer.observe(card)), 0)
      cancelObserve = () => clearTimeout(timeout)
    }
    return () => {
      observer.disconnect()
      cancelObserve()
      // Preserve track spans across React updates: collapsing the grid even
      // briefly clamps the document's scroll position before we can remeasure.
    }
  }, [cards, masonry, rowGap])
  return <div {...props} data-masonry={masonry} ref={collection}>{children}</div>
}
