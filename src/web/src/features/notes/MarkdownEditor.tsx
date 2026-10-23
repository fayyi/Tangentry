import { Button } from '../../components/ui'
import { Bold, Code, Eye, Heading2, Italic, Link, List, Quote, Text, Pencil } from 'lucide-react'
import { useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { MarkdownBody } from './MarkdownBody'

// Shared editing surface. Draft storage, anchors and persistence belong to callers.
export function MarkdownEditor({ id, label, value, onChange, editorRef, preview, onPreviewChange, pending, invalid, describedBy, onSave, rows = 7, ariaLabel = '笔记正文', compact = false, placeholder = '写下想法…支持 Markdown' }: {
  id: string; label: string; value: string; onChange: (value: string) => void
  editorRef: RefObject<HTMLTextAreaElement | null>; preview: boolean; onPreviewChange: (value: boolean) => void
  pending: boolean; invalid?: boolean; describedBy?: string; onSave: () => void; rows?: number; ariaLabel?: string; compact?: boolean; placeholder?: string
}) {
  const [formatting, setFormatting] = useState(false)
  const selection = useRef<{ value: string; start: number; end: number } | null>(null)
  useLayoutEffect(() => {
    const requested = selection.current
    selection.current = null
    const node = editorRef.current
    if (!requested || !node || node.value !== requested.value) return
    node.focus({ preventScroll: true })
    node.setSelectionRange(requested.start, requested.end)
  }, [value, editorRef])
  function format(before: string, after = '', fallback = '文字') {
    const node = editorRef.current
    if (!node || pending) return
    const start = node.selectionStart, end = node.selectionEnd
    if (before.endsWith(' ')) {
      const lineStart = start === 0 ? 0 : value.lastIndexOf('\n', start - 1) + 1
      // A selection ending at the next line's start does not include that line.
      const lastSelected = end > start && value[end - 1] === '\n' ? end - 1 : end
      const nextBreak = value.indexOf('\n', lastSelected)
      const lineEnd = nextBreak === -1 ? value.length : nextBreak
      const lines = value.slice(lineStart, lineEnd).split('\n')
      const remove = lines.every(line => line.startsWith(before))
      const replacement = lines.map(line => remove ? line.slice(before.length) : line.startsWith(before) ? line : before + line).join('\n')
      const next = value.slice(0, lineStart) + replacement + value.slice(lineEnd)
      selection.current = { value: next, start: lineStart, end: lineStart + replacement.length }
      onChange(next)
      return
    }
    const selectedText = value.slice(start, end) || fallback
    const next = value.slice(0, start) + before + selectedText + after + value.slice(end)
    selection.current = { value: next, start: start + before.length, end: start + before.length + selectedText.length }
    onChange(next)
  }
  return <>
    <div className="note-editor-heading"><label htmlFor={id}>{label}</label><div className="note-mode" aria-label="笔记显示模式">{compact ? <><Button variant="quiet" type="button" aria-label="格式工具" title="格式工具" aria-expanded={formatting} aria-controls={`${id}-format`} onClick={() => { setFormatting(!formatting); onPreviewChange(false) }}><Text size={15}/></Button><Button variant="quiet" type="button" aria-label={preview ? '编辑' : '预览'} title={preview ? '编辑' : '预览'} aria-pressed={preview} onClick={() => onPreviewChange(!preview)}>{preview ? <Pencil size={15}/> : <Eye size={15}/>}</Button></> : <><Button variant="quiet" type="button" aria-pressed={!preview} onClick={() => onPreviewChange(false)}>编辑</Button><Button variant="quiet" type="button" aria-pressed={preview} onClick={() => onPreviewChange(true)}>预览</Button></>}</div></div>
    {!preview && (!compact || formatting) && <div id={`${id}-format`} className="note-format-tools" role="group" aria-label="Markdown 格式"><Button variant="quiet" type="button" aria-label="加粗" title="加粗" disabled={pending} onClick={() => format('**', '**')}><Bold size={14}/></Button><Button variant="quiet" type="button" aria-label="斜体" title="斜体" disabled={pending} onClick={() => format('*', '*')}><Italic size={14}/></Button><Button variant="quiet" type="button" aria-label="标题" title="标题" disabled={pending} onClick={() => format('## ')}><Heading2 size={15}/></Button><Button variant="quiet" type="button" aria-label="引用" title="引用" disabled={pending} onClick={() => format('> ')}><Quote size={14}/></Button><Button variant="quiet" type="button" aria-label="列表" title="列表" disabled={pending} onClick={() => format('- ')}><List size={15}/></Button><Button variant="quiet" type="button" aria-label="代码" title="代码" disabled={pending} onClick={() => format('`', '`', 'code')}><Code size={15}/></Button><Button variant="quiet" type="button" aria-label="链接" title="链接" disabled={pending} onClick={() => format('[', '](https://)', '链接文字')}><Link size={14}/></Button><span>Markdown</span></div>}
    {preview ? <div className="note-preview" role="region" aria-label="笔记 Markdown 预览">{value ? <MarkdownBody>{value}</MarkdownBody> : <p className="notes-hint">写点什么，再来预览。</p>}</div> : <textarea id={id} ref={editorRef} aria-label={ariaLabel} value={value} maxLength={16000} rows={rows} placeholder={placeholder} disabled={pending} aria-invalid={invalid} aria-describedby={describedBy} onChange={event => onChange(event.target.value)} onKeyDown={event => {
      if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229 || event.repeat || event.altKey) return
      if (event.metaKey || event.ctrlKey) {
        if (event.key === 'Enter') { event.preventDefault(); onSave() }
        else if (event.key.toLowerCase() === 'b') { event.preventDefault(); format('**', '**') }
        else if (event.key.toLowerCase() === 'i') { event.preventDefault(); format('*', '*') }
      }
    }}/>}
  </>
}
