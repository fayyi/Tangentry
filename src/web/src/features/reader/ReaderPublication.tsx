import { useEffect, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { cachedPublication, readPublication } from '../../shared/data/publicationDetails'
import { publicationText } from '../../shared/ui/ContentCard'

export function ReaderPublication({ item }: { item: ContentItem }) {
  const [resolved, setResolved] = useState(() => cachedPublication(item.id))
  const [checked, setChecked] = useState(false)

  useEffect(() => {
    setResolved(cachedPublication(item.id))
    setChecked(false)
    if (publicationText(item) || item.platform !== 'youtube') return
    let active = true
    void readPublication(item).then(value => {
      if (active) {
        setResolved(value)
        setChecked(true)
      }
    })
    return () => { active = false }
  }, [item.id, item.url, item.platform, item.publishedAt, item.publishedText])

  return <>{publicationText(item) || (resolved && publicationText(resolved)) || (item.platform === 'youtube' && !checked ? '日期待获取' : '发布时间未知')}</>
}
