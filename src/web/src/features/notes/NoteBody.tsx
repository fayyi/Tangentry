import { ChevronDown, ChevronUp } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { MarkdownBody } from './MarkdownBody'
import type { TaskControls } from './taskList'

export function NoteBody({ id, body, active, bodyClassName = 'note-body', ...tasks }: { id: string; body: string; active: boolean; bodyClassName?: string } & TaskControls) {
  const content = useRef<HTMLDivElement>(null)
  const [overflow, setOverflow] = useState(false)
  const [expanded, setExpanded] = useState(false)
  useEffect(() => {
    const node = content.current
    if (!node) return
    const measure = () => setOverflow(node.scrollHeight > 176)
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    measure()
    return () => observer.disconnect()
  }, [body])
  const open = expanded || active
  return <div className="note-reading-body" data-collapsed={overflow && !open}>
    <div id={`note-content-${id}`} className="note-body-content" onFocusCapture={() => setExpanded(true)}>
      <div ref={content} className="note-body-measure"><MarkdownBody className={bodyClassName} {...tasks}>{body}</MarkdownBody></div>
    </div>
    {overflow && !active && <button type="button" className="note-expand" aria-expanded={open} aria-controls={`note-content-${id}`} onClick={() => setExpanded(!expanded)}>{open ? <ChevronUp size={14}/> : <ChevronDown size={14}/>}{open ? '收起笔记' : '展开全文'}</button>}
  </div>
}
