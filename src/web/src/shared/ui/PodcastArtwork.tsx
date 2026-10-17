import { Headphones } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ContentItem } from '../api/types'
import { cachedPublication, readPublication } from '../data/publicationDetails'

export function PodcastArtwork({ item, className = 'card-audio-cover', resolveMissing = true }: { item: ContentItem; className?: string; resolveMissing?: boolean }) {
  const node = useRef<HTMLDivElement>(null)
  const [url, setURL] = useState(() => item.thumbnailUrl || cachedPublication(item.id)?.thumbnailUrl || '')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!resolveMissing || item.platform !== 'xiaoyuzhou' || (url && !failed)) return
    let active = true
    const observer = new IntersectionObserver(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return
      observer.disconnect()
      void readPublication(item, false, false, true).then(detail => {
        if (active && detail?.thumbnailUrl && detail.thumbnailUrl !== url) {
          setURL(detail.thumbnailUrl)
          setFailed(false)
        }
      })
    }, { rootMargin: '100px' })
    if (node.current) observer.observe(node.current)
    return () => { active = false; observer.disconnect() }
  }, [item.id, item.url, item.platform, url, failed, resolveMissing])
  return <div ref={node} className={className}>
    {url && !failed
      ? <img src={url} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)} />
      : <div className="thumbnail-placeholder"><Headphones size={32} aria-hidden="true" /></div>}
  </div>
}
