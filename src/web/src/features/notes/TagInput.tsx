import { Hash } from 'lucide-react'
import { tagValidation } from './tags'
export { parseTags, tagValidation } from './tags'

export function TagInput({ id, value, onChange, disabled }: { id: string; value: string; onChange: (value: string) => void; disabled: boolean }) {
  const error = tagValidation(value)
  return <div className="note-tag-field"><label className="note-tag-input" htmlFor={id}><Hash size={14} aria-hidden="true"/><span>标签</span><input id={id} aria-label="笔记标签，逗号分隔" value={value} onChange={event => onChange(event.target.value)} disabled={disabled} maxLength={512} placeholder="学习/阅读, 灵感" aria-invalid={Boolean(error)} aria-describedby={error ? `${id}-error` : undefined} /></label>{error && <p id={`${id}-error`} className="note-tag-error" role="status">{error}</p>}</div>
}
