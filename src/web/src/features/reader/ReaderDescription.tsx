import { useEffect, useMemo, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { cachedPublication, readPublication } from '../../shared/data/publicationDetails'
import { TextTranslationControl, useTextTranslation } from '../ai/TextTranslation'
import { isChineseText } from '../../shared/text/chineseText'
import { DescriptionText } from './DescriptionText'
import { descriptionTranslationParts } from './descriptionTranslation'

export function ReaderDescription({ item, onSeek, className = '' }: { item: ContentItem; onSeek?: (seconds: number) => void; className?: string }) {
  const [resolved, setResolved] = useState(() => cachedPublication(item.id)?.description ?? '')
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    setResolved(cachedPublication(item.id)?.description ?? '')
    setLoading(false)
    if (item.description?.trim() || item.platform !== 'youtube') return
    let active = true
    setLoading(true)
    void readPublication(item, false, true).then(value => {
      if (active) { setResolved(value?.description ?? ''); setLoading(false) }
    })
    return () => { active = false }
  }, [item.id, item.url, item.platform, item.description])
  const text = item.description?.trim() || resolved
  const translatable = Boolean(text) && !isChineseText(text)
  const { sentences, pieces, ranges } = useMemo(() => descriptionTranslationParts(text), [text])
  const translation = useTextTranslation(item.id, pieces, 'article', translatable)
  const translated = translation.show ? translation.inspection?.result.texts : undefined
  return <details className={`reader-description ${className}`}>
    <summary>{item.kind === 'audio' ? '节目说明' : '内容简介'}</summary>
    {translatable && <TextTranslationControl translation={translation} label={item.kind === 'audio' ? '翻译节目说明' : '翻译简介'}/>}
    {translated?.length ? <div className="description-bilingual">{sentences.map((sentence, index) => {
      if (!sentence.trim()) return /\n\s*\n/.test(sentence) ? <div key={index} className="description-paragraph-break" aria-hidden="true"/> : null
      const { start, end } = ranges[index]
      const chinese = translated.slice(start, end).join('')
      return <div key={index} className="description-pair">
        <p className="description-original"><DescriptionText text={sentence} onSeek={onSeek}/></p>
        {chinese && <p className="description-translation" lang="zh-Hans"><DescriptionText text={chinese} onSeek={onSeek}/></p>}
      </div>
    })}</div> : <p><DescriptionText text={text || (loading ? '正在获取简介…' : '暂未获取到简介，可打开原内容查看。')} onSeek={onSeek}/></p>}
  </details>
}
