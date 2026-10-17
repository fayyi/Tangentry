export const parseTags = (value: string): string[] => value.split(/[,，\s]+/u).filter(Boolean)

export const validTag = (tag: string): boolean =>
  Array.from(tag).length <= 32 && /^[\p{L}\p{N}_-]+(?:\/[\p{L}\p{N}_-]+)*$/u.test(tag)

export const matchesNoteTag = (tags: string[] | undefined, selected: string): boolean =>
  !selected || Boolean(tags?.some(tag => tag === selected || tag.startsWith(selected + '/')))

export function tagValidation(value: string): string {
  const tags = parseTags(value)
  if (tags.length > 12) return '最多填写 12 个标签。'
  for (const raw of tags) {
    // JavaScript casing can expand a rune; the Go service owns normalization.
    const tag = raw.replace(/^#/, '')
    if (Array.from(tag).length > 32) return '每个标签最多 32 个字符。'
    if (!validTag(tag)) return '只能包含文字、数字、_、-、/。'
  }
  return ''
}
