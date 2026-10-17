import type { SyntheticEvent } from 'react'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex } from '@noble/hashes/utils.js'
import type { ContentItem } from '../api/types'

type ImageOwner = Pick<ContentItem, 'id' | 'platform'> & Partial<Pick<ContentItem, 'url'>>

// Xiaohongshu images need the collected note's credentials, so they always
// load through the local server. Other images load directly.
export function sourceImageURL(item: ImageOwner, raw: string): string {
  if (!raw || item.platform !== 'xiaohongshu') return raw
  const version = bytesToHex(sha256(new TextEncoder().encode(raw)))
  return '/api/xiaohongshu/images/' + encodeURIComponent(item.id) + '/' + version
}

// proxiedImageURL loads a public image through the local proxy, which sends a
// browser identity and the article's origin as many image hosts require.
export const proxiedImageURL = (raw: string, referrer = '') =>
  `/api/articles/image?url=${encodeURIComponent(raw)}&referrer=${encodeURIComponent(referrer)}`

// retryThroughProxy is an <img> onError step: the first failure of a direct
// public image retries it through the proxy. It returns false once the image
// has really failed, so callers can show their fallback.
export function retryThroughProxy(event: SyntheticEvent<HTMLImageElement>, item: ImageOwner, raw: string): boolean {
  const image = event.currentTarget
  if (item.platform === 'xiaohongshu' || !/^https?:\/\//.test(raw) || image.dataset.proxied) return false
  image.dataset.proxied = 'true'
  image.src = proxiedImageURL(raw, item.url)
  return true
}
