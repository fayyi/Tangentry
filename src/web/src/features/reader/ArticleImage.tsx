import { ImageOff } from 'lucide-react'
import { useState } from 'react'
import type { ArticleBlock } from '../../shared/api/types'
import { proxiedImageURL } from '../../shared/media/sourceImage'

// Article images are stored as links and always load through the local proxy.

export function ArticleImage({ block, referrer }: { block: ArticleBlock; referrer?: string }) {
  const [failed, setFailed] = useState(false)
  const url = block.url ?? ''
  if (failed) return <figure className={`article-block article-block-image article-block-${block.kind} article-image-failed`}>
    {/* The label is drawn by CSS: text nodes inside the article are the
        annotation model, so a fallback must not add characters to it. */}
    <a href={url} target="_blank" rel="noreferrer" aria-label={'图片未能加载，打开原图' + (block.alt ? `：${block.alt}` : '')} data-label="图片未能加载，打开原图"><ImageOff size={15} aria-hidden="true"/></a>
  </figure>
  return <figure className={`article-block article-block-image article-block-${block.kind}`}>
    <img src={proxiedImageURL(url, referrer)} alt={block.alt ?? ''} width={block.width || undefined} height={block.height || undefined} loading="lazy" decoding="async" referrerPolicy="no-referrer" onError={() => setFailed(true)}/>
  </figure>
}
