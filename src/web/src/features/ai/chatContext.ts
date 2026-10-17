const DEFAULT_CONTEXT_LIMIT = 120_000

export function buildChatContext(source: string, overview?: string, points?: string[], limit = DEFAULT_CONTEXT_LIMIT): string {
  const material = [overview, ...(points ?? [])].map(value => value?.trim()).filter(Boolean).join('\n')
  const raw = source.trim()
  const combined = [material, raw].filter(Boolean).join('\n\n')
  if (combined.length <= limit) return combined
  const summary = material.slice(0, Math.min(material.length, Math.floor(limit * 0.35)))
  const remaining = Math.max(0, limit - summary.length - 40)
  const head = Math.ceil(remaining / 2)
  const tail = remaining - head
  return [summary, '相关内容片段：', raw.slice(0, head), '……', raw.slice(Math.max(0, raw.length - tail))].filter(Boolean).join('\n')
}
