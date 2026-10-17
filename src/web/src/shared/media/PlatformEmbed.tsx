import type { ContentItem } from '../api/types'

export function platformEmbedURL(item: Pick<ContentItem, 'platform' | 'platformId'>, autoplay = false): string | null {
  if (item.platform === 'bilibili' && /^BV[0-9A-Za-z]{10}$/.test(item.platformId)) return `https://player.bilibili.com/player.html?bvid=${item.platformId}&page=1&high_quality=1&autoplay=${autoplay ? 1 : 0}`
  if (item.platform === 'youtube' && /^[A-Za-z0-9_-]{11}$/.test(item.platformId)) return `https://www.youtube-nocookie.com/embed/${item.platformId}?autoplay=${autoplay ? 1 : 0}&playsinline=1&rel=0`
  return null
}

export function PlatformEmbed({ item, autoplay = false }: { item: Pick<ContentItem, 'platform' | 'platformId' | 'title'>; autoplay?: boolean }) {
  const src = platformEmbedURL(item, autoplay)
  return src ? <iframe className="platform-video" title={'在线视频播放器 ' + item.title} src={src} allow="autoplay; fullscreen; picture-in-picture; encrypted-media" allowFullScreen referrerPolicy="strict-origin-when-cross-origin" /> : null
}
