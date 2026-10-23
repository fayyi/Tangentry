import { Input } from '../../components/ui'
import { createContext, useContext, useEffect, useId, useRef, type ComponentProps } from 'react'
import Markdown, { type Components, type ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { taskSourcePositions, type TaskControls } from './taskList'
import { proxiedImageURL } from '../../shared/media/sourceImage'
import './markdown.css'

// Notes remain Markdown source in storage. No raw HTML plugin or implicit image
// requests: external media is a link that the reader deliberately opens.
const TaskContext = createContext<TaskControls>({})
function TaskInput({ node, checked, ...props }: ComponentProps<'input'> & ExtraProps) {
  const { onTaskChange, tasksDisabled } = useContext(TaskContext)
  const offset = node?.properties['data-task-offset']
  const editable = typeof offset === 'number' && Boolean(onTaskChange)
  const input = useRef<HTMLInputElement>(null)
  const restoreFocus = useRef(false)
  useEffect(() => {
    if (tasksDisabled) {
      const moved = () => { restoreFocus.current = false }
      document.addEventListener('pointerdown', moved)
      return () => document.removeEventListener('pointerdown', moved)
    }
    if (restoreFocus.current && document.activeElement === document.body) input.current?.focus({ preventScroll: true })
    restoreFocus.current = false
  }, [tasksDisabled])
  return <label className={editable ? 'markdown-task-control' : undefined}>
    <Input {...props} ref={input} aria-label={props['aria-label'] || '待办事项'} checked={Boolean(checked)} disabled={!editable || tasksDisabled} title={editable ? checked ? '标为未完成' : '标为完成' : undefined} onChange={event => {
      if (!editable || tasksDisabled) return
      restoreFocus.current = document.activeElement === input.current
      onTaskChange?.(offset as number, event.target.checked)
    }} />
  </label>
}

const components: Components = {
  p: ({ children }) => <p>{children}{'\n\n'}</p>,
  h1: ({ children }) => <h1>{children}{'\n\n'}</h1>,
  h2: ({ children }) => <h2>{children}{'\n\n'}</h2>,
  h3: ({ children }) => <h3>{children}{'\n\n'}</h3>,
  h4: ({ children }) => <h4>{children}{'\n\n'}</h4>,
  h5: ({ children }) => <h5>{children}{'\n\n'}</h5>,
  h6: ({ children }) => <h6>{children}{'\n\n'}</h6>,
  blockquote: ({ children }) => <blockquote>{children}{'\n\n'}</blockquote>,
  li: ({ children }) => <li>{children}{'\n'}</li>,
  a: ({ href, children }) => href ? <a href={href} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>,
  img: ({ src, alt }) => <span className="markdown-image-link">{src ? <a href={src} target="_blank" rel="noopener noreferrer">{alt || '查看图片'} ↗</a> : alt || '图片'}</span>,
  table: ({ children }) => <div className="markdown-table"><table>{children}</table></div>,
  input: TaskInput,
}

export default function MarkdownRenderer({ children, className = '', onTaskChange, tasksDisabled = false, imageMode = 'link', imageReferrer = '' }: { children: string; className?: string; imageMode?: 'link' | 'inline'; imageReferrer?: string } & TaskControls) {
  const prefix = useId().replaceAll(':', '')
  const renderComponents = imageMode === 'inline' ? { ...components, img: ({ src, alt }: { src?: string; alt?: string }) => {
    let resolved = src ?? ''
    try {
      if (imageReferrer) resolved = new URL(resolved, imageReferrer).href
    } catch { resolved = '' }
    if (!/^https?:\/\//i.test(resolved)) return <span className="markdown-image-link">{alt || '图片'}</span>
    return <img src={proxiedImageURL(resolved, imageReferrer)} alt={alt || ''} loading="lazy" decoding="async" referrerPolicy="no-referrer" />
  } } : components
  return <TaskContext.Provider value={{ onTaskChange, tasksDisabled }}><div className={`markdown-body ${className}`}><Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[taskSourcePositions]} remarkRehypeOptions={{ clobberPrefix: `memo-${prefix}-`, footnoteLabel: '脚注', footnoteBackLabel: '返回正文' }} components={renderComponents}>{children}</Markdown></div></TaskContext.Provider>
}
