import { useState } from 'react'
import type { ContentItem, Feed } from '../../shared/api/types'

interface SourceFeed { source: string; feed: Feed | null; label: string }
interface Entry { item: ContentItem; origin: string }

export function isCachedContinuation(previous: Feed, next: Feed): boolean {
  return (next.page === 0 || previous.page === 0 && next.page === 1) && next.items.length >= previous.items.length &&
    previous.items.every((item, index) => next.items[index]?.id === item.id)
}

export function mixRecommendations(feeds: SourceFeed[], randomize = false): Entry[] {
  const seen = new Set<string>(), entries: Entry[] = []
  const shuffle = <T,>(values: T[]): T[] => {
    for (let index = values.length - 1; index > 0; index--) {
      const other = Math.floor(Math.random() * (index + 1))
      ;[values[index], values[other]] = [values[other], values[index]]
    }
    return values
  }
  const queues = feeds.map(value => {
    const items = (value.feed?.items ?? []).filter(item => {
      if (seen.has(item.id)) return false
      seen.add(item.id)
      return true
    })
    return { ...value, items: randomize ? shuffle(items) : items }
  })
  const sources = randomize ? shuffle(queues) : queues
  for (let index = 0; index < Math.max(0, ...sources.map(value => value.items.length)); index++) {
    for (const { feed, label, items } of sources) {
      const item = items[index]
      if (item) {
        entries.push({ item, origin: feed?.sourceTitle || label })
      }
    }
  }
  return entries
}

export function useRecommendationOrder(feeds: SourceFeed[], scope: string): Entry[] {
  const [snapshot, setSnapshot] = useState<{ scope: string; feeds: SourceFeed[]; entries: Entry[] }>(() => ({ scope, feeds, entries: mixRecommendations(feeds, scope !== 'subscriptions') }))
  if (snapshot.scope === scope && feeds.length === snapshot.feeds.length && feeds.every((value, index) => value.source === snapshot.feeds[index]?.source && value.feed === snapshot.feeds[index]?.feed)) return snapshot.entries
  const mixed = mixRecommendations(feeds, scope !== 'subscriptions')
  const byId = new Map(mixed.map(entry => [entry.item.id, entry]))
  // Explicit refreshes replace a page-one feed. Appends and delayed sources
  // preserve the visible prefix, so new cards cannot jump ahead of the reader.
  const reset = snapshot.scope !== scope || feeds.some(value => {
    const previous = snapshot.feeds.find(candidate => candidate.source === value.source)?.feed
    return previous && value.feed !== previous && (!value.feed || value.feed.page <= 1 && !isCachedContinuation(previous, value.feed))
  })
  const retained = reset ? [] : snapshot.entries.filter(entry => byId.has(entry.item.id)).map(entry => byId.get(entry.item.id)!)
  const existing = new Set(retained.map(entry => entry.item.id))
  const additions = mixRecommendations(feeds.map(value => ({ ...value, feed: value.feed
    ? { ...value.feed, items: value.feed.items.filter(item => !existing.has(item.id)) }
    : null })), scope !== 'subscriptions')
  const entries = [...retained, ...additions]
  setSnapshot({ scope, feeds, entries })
  return entries
}
