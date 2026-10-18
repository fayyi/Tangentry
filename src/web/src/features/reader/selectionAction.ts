export interface SelectionActionPosition {
  top: number
  left: number
}

export function selectionActionPosition(rect: Pick<DOMRect, 'top' | 'bottom' | 'left' | 'right'>, viewport: { width: number; height: number }, action = { width: 210, height: 40 }): SelectionActionPosition {
  const margin = 8
  const left = Math.min(Math.max(margin, (rect.left + rect.right - action.width) / 2), Math.max(margin, viewport.width - action.width - margin))
  const above = rect.top - action.height - margin
  const top = above >= margin ? above : Math.min(rect.bottom + margin, Math.max(margin, viewport.height - action.height - margin))
  return { top, left }
}
