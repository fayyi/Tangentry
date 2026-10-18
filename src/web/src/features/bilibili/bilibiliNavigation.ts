export type BilibiliTab = 'popular' | 'ranking' | 'dynamic' | 'live' | 'my'
export type BilibiliArea = '1' | '2' | '9'

const tabs: readonly BilibiliTab[] = ['popular', 'ranking', 'dynamic', 'live', 'my']
const liveAreas: readonly BilibiliArea[] = ['1', '2', '9']

/** Keep the B站 sub-view in the current history entry rather than adding one per control click. */
export function bilibiliPath(tab: BilibiliTab, area: string = '1') {
  const params = new URLSearchParams({ tab })
  if (tab === 'live' && liveAreas.includes(area as BilibiliArea)) params.set('area', area)
  return `/bilibili?${params}`
}

export function bilibiliStateFromURL(path = typeof window === 'undefined' ? '/bilibili' : window.location.href): { tab: BilibiliTab; area: BilibiliArea } {
  const params = new URL(path, 'http://tangentry.local').searchParams
  const value = params.get('tab')
  const tab = tabs.includes(value as BilibiliTab) ? value as BilibiliTab : 'popular'
  const valueArea = params.get('area')
  const area = liveAreas.includes(valueArea as BilibiliArea) ? valueArea as BilibiliArea : '1'
  return { tab, area }
}

