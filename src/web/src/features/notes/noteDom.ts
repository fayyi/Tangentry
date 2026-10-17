export function revealNote(node: HTMLElement) {
  const records = node.closest<HTMLElement>('.note-records-workspace')
  if (records && getComputedStyle(records).overflowY === 'auto') {
    const item = node.getBoundingClientRect()
    const region = records.getBoundingClientRect()
    records.scrollTo({ top: records.scrollTop + item.top - region.top - records.clientTop - (records.clientHeight - item.height) / 2, behavior: 'instant' })
  } else node.scrollIntoView({ block: 'center', behavior: 'instant' })
}
