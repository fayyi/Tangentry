import { AlertCircle, Check, Info } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { shouldCloseToolbarPopoverForPointer, shouldCloseToolbarPopoverOnKey, shouldOpenToolbarPopoverOnFocus } from './toolbarPopoverState'
import './toolbar-popover.css'

export type ToolbarPopoverTone = 'neutral' | 'success' | 'warning' | 'danger'

type ToolbarPopoverProps = {
  label: string
  title?: string
  tone?: ToolbarPopoverTone
  icon?: ReactNode
  showLabel?: boolean
  children: ReactNode
  announcement?: string
}

const defaultIcons = {
  neutral: <Info size={15} aria-hidden="true" />,
  success: <Check size={15} aria-hidden="true" />,
  warning: <Info size={15} aria-hidden="true" />,
  danger: <AlertCircle size={15} aria-hidden="true" />,
} satisfies Record<ToolbarPopoverTone, ReactNode>

export function ToolbarPopover({ label, title, tone = 'neutral', icon, showLabel = false, children, announcement }: ToolbarPopoverProps) {
  const id = useId()
  const root = useRef<HTMLSpanElement>(null), trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)

  const close = () => {
    setOpen(false)
    window.setTimeout(() => trigger.current?.focus(), 0)
  }

  useEffect(() => {
    if (!open) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (!shouldCloseToolbarPopoverOnKey(event.key)) return
      event.preventDefault()
      close()
    }
    const closeOnOutsidePointer = (event: PointerEvent) => {
      if (shouldCloseToolbarPopoverForPointer(Boolean(root.current?.contains(event.target as Node)))) close()
    }
    document.addEventListener('keydown', closeOnEscape)
    document.addEventListener('pointerdown', closeOnOutsidePointer)
    return () => {
      document.removeEventListener('keydown', closeOnEscape)
      document.removeEventListener('pointerdown', closeOnOutsidePointer)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const firstAction = panel.current?.querySelector<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')
    if (firstAction) firstAction.focus()
    else if (panel.current) {
      panel.current.tabIndex = -1
      panel.current.focus()
    }
  }, [open])

  const liveRole = tone === 'danger' ? 'alert' : 'status'
  return <span ref={root} className={`toolbar-popover toolbar-popover-${tone}`} onBlur={event => {
    const next = event.relatedTarget as Node | null
    if (!next || !root.current?.contains(next)) close()
  }}>
    {announcement && <span className="visually-hidden" role={liveRole} aria-live={liveRole === 'alert' ? 'assertive' : 'polite'}>{announcement}</span>}
    <button ref={trigger} type="button" className={`toolbar-popover-trigger${showLabel ? ' toolbar-popover-trigger-labeled' : ''}`} aria-label={label} aria-haspopup="dialog" aria-expanded={open} aria-controls={id} title={title || label} onFocus={event => {
      if (shouldOpenToolbarPopoverOnFocus(event.currentTarget.matches(':focus-visible'))) setOpen(true)
    }} onClick={() => setOpen(true)}>
      {icon || defaultIcons[tone]}
      {showLabel && <span>{label}</span>}
    </button>
    {open && <div id={id} ref={panel} className="toolbar-popover-panel" role="dialog" aria-label={label} onClick={event => {
      const target = event.target as HTMLElement
      if (target.closest('button:not(:disabled), a[href]')) close()
    }}>{children}</div>}
  </span>
}
