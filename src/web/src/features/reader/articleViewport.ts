export function originalRuns(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>('[data-article-start]')].filter(node => node.textContent?.trim())
}

export function readingRun(nodes: HTMLElement[]): { offset: number; top: number } | null {
  let low = 0, high = nodes.length
  // Original runs follow document order, including highlighted portions of a paragraph.
  while (low < high) {
    const middle = Math.floor((low + high) / 2)
    if (nodes[middle].getBoundingClientRect().bottom <= 96) low = middle + 1
    else high = middle
  }
  const node = nodes[low]
  if (!node) return null
  const rect = node.getBoundingClientRect()
  if (!rect.width || !rect.height || rect.top >= innerHeight) return null
  const offset = Number(node.dataset.articleStart)
  return Number.isInteger(offset) && offset >= 0 ? { offset, top: rect.top } : null
}

export function originalRunAt(root: HTMLElement, offset: number): HTMLElement | undefined {
  return originalRuns(root).find(node => Number(node.dataset.articleStart) <= offset && Number(node.dataset.articleEnd) > offset)
}
