import { Children, isValidElement, useLayoutEffect, useRef } from 'react'
import type { HTMLAttributes } from 'react'

export function CardCollection({ children, masonry = true, rowGap = 16, ...props }: HTMLAttributes<HTMLDivElement> & { masonry?: boolean; rowGap?: number }) {
  const collection = useRef<HTMLDivElement>(null)
  // Re-measure when the set of cards changes, not on every parent render:
  // ResizeObserver already follows each card's own height changes.
  const cards = Children.toArray(children).map(child => isValidElement(child) ? String(child.key) : '').join('\n')
  useLayoutEffect(() => {
    const node = collection.current
    if (!node) return
    const scrollTop = window.scrollY
    const cards = Array.from(node.children) as HTMLElement[]
    if (!masonry) {
      cards.forEach(card => card.style.removeProperty('grid-row-end'))
      return
    }
    // One-pixel tracks let each card keep its own height and fill earlier gaps.
    // Row spacing follows the user's card spacing relative to the standard 18px column gap.
    const gap = parseFloat(getComputedStyle(node).columnGap)
    const spacing = Number.isFinite(gap) && gap > 0 ? Math.max(4, rowGap + gap - 18) : rowGap
    const resize = (entries: { target: Element }[]) => {
      const sizes = entries.map(({ target }) => ({
        card: target as HTMLElement,
        rows: Math.ceil(target.getBoundingClientRect().height + spacing),
      }))
      for (const { card, rows } of sizes) card.style.gridRowEnd = `span ${rows}`
    }
    // Activity hides and later reveals the collection by restarting effects while
    // preserving the DOM. Existing spans keep the page geometry stable, so let
    // ResizeObserver refresh them asynchronously instead of synchronously
    // measuring every card during reveal.
    const hasExistingSpans = cards.length > 0 && cards.every(card => card.style.gridRowEnd)
    if (!hasExistingSpans) resize(cards.map(target => ({ target })))
    if (Math.abs(window.scrollY - scrollTop) > 1) window.scrollTo({ top: scrollTop, behavior: 'auto' })
    const observer = new ResizeObserver(resize)
    cards.forEach(card => observer.observe(card))
    return () => {
      observer.disconnect()
      // Preserve track spans across React updates: collapsing the grid even
      // briefly clamps the document's scroll position before we can remeasure.
    }
  }, [cards, masonry, rowGap])
  return <div {...props} data-masonry={masonry} ref={collection}>{children}</div>
}
