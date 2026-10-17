type Source = 'feed' | 'original'
const key = ['tangentry', 'article-source', 'v1'].join('.')

function entries(): { id: string; source: Source }[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? '[]')
    return Array.isArray(value) ? value.filter(entry => entry && typeof entry.id === 'string' &&
      entry.id.startsWith('rss:') && (entry.source === 'feed' || entry.source === 'original')).slice(-128) : []
  } catch { return [] }
}

export function articleSource(id: string): Source | undefined {
  return entries().find(entry => entry.id === id)?.source
}

export function rememberArticleSource(id: string, source: Source) {
  try {
    localStorage.setItem(key, JSON.stringify([...entries().filter(entry => entry.id !== id).slice(-127), { id, source }]))
  } catch { /* Source selection remains usable without optional storage. */ }
}
