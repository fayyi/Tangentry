export function rangeSelection(ids: string[], anchor: string | null, target: string): string[] {
  const targetIndex = ids.indexOf(target)
  if (targetIndex < 0) return []
  const anchorIndex = anchor ? ids.indexOf(anchor) : -1
  if (anchorIndex < 0) return [target]
  const [start, end] = anchorIndex <= targetIndex ? [anchorIndex, targetIndex] : [targetIndex, anchorIndex]
  return ids.slice(start, end + 1)
}

export function toggleSelection(current: Iterable<string>, ids: string[], id: string, range = false, anchor: string | null = null): Set<string> {
  const next = new Set(current)
  if (range) {
    rangeSelection(ids, anchor, id).forEach(value => next.add(value))
    return next
  }
  if (next.has(id)) next.delete(id)
  else next.add(id)
  return next
}
