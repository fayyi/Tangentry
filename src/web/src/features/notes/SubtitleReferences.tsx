import { Button } from '../../components/ui'
import { Clock3, X } from 'lucide-react'
import type { SubtitleReference, SubtitleSourceVersion } from '../../shared/api/types'
import { noteOriginalAt } from './source'
import { subtitleClock } from './subtitleReferenceData'

export { sameSubtitleReference, subtitleReferenceValid } from './subtitleReferenceData'
export { subtitleClock } from './subtitleReferenceData'

export function SubtitleReferences({ references, contentId, sourceVersion, onSeek, onRemove, disabled = false }: {
  references?: SubtitleReference[]; contentId: string; sourceVersion?: SubtitleSourceVersion | null; onSeek?: (position: number) => void; onRemove?: (index: number) => void; disabled?: boolean
}) {
  if (!references?.length) return null
  return <div className="note-subtitle-references" aria-label="字幕摘录">
    {references.map((reference, index) => {
      const label = `${subtitleClock(reference.start)}–${subtitleClock(reference.end)}`
      const original = noteOriginalAt(contentId, reference.start)
      return <div className="note-subtitle-reference" key={index}>
        <div className="note-subtitle-reference-meta">
          {onSeek ? <Button variant="quiet" type="button" disabled={disabled} aria-label={`回看字幕摘录 ${label}`} title="回到这段字幕" onClick={() => onSeek(reference.start)}><Clock3 size={13} aria-hidden="true"/>{label}</Button>
            : original ? <a href={original} target="_blank" rel="noopener noreferrer" aria-label={`在原视频回看字幕摘录 ${label}`}><Clock3 size={13} aria-hidden="true"/>{label}</a> : <span>{label}</span>}
          <span>{reference.language}</span>
          {reference.trackHash && sourceVersion?.language === reference.language && sourceVersion.hash !== reference.trackHash && <span title="字幕已更新，这里保留摘录时的原文。">原稿已更新</span>}
          {onRemove && <Button variant="quiet" type="button" className="note-subtitle-remove" disabled={disabled} title="移除摘录" aria-label={`移除字幕摘录 ${label}`} onClick={() => onRemove(index)}><X size={14}/></Button>}
        </div>
        <blockquote>{reference.text}</blockquote>
      </div>
    })}
  </div>
}
