import { Button } from '../../components/ui'
import { FileText, Headphones, Images, LayoutGrid, Play, BookOpen } from 'lucide-react'

const media = [
  ['all', '全部', LayoutGrid],
  ['video', '视频', Play],
  ['article', '图文', BookOpen],
  ['image', '短图文', Images],
  ['audio', '音频', Headphones],
  ['pdf', 'PDF', FileText],
] as const

export function MediaFilters({ value, onChange, counts }: { value: string; onChange: (value: string) => void; counts?: Partial<Record<string, number>> }) {
  return <div className="media-filters gallery-filters" role="group" aria-label="媒体类型">{media.map(([id, label, Icon]) => <Button variant="quiet" key={id} type="button" aria-label={label} aria-pressed={value === id} onClick={() => onChange(id)}><Icon size={15} strokeWidth={1.7}/><span>{label}</span>{counts?.[id] !== undefined && <small className="media-filter-count" aria-hidden="true">{counts[id]}</small>}</Button>)}</div>
}
