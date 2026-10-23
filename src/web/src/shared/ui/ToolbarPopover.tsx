import { Button } from '../../components/ui'
import { AlertCircle, Check, Info } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { shouldCloseToolbarPopoverForPointer, shouldCloseToolbarPopoverOnKey, shouldOpenToolbarPopoverOnFocus, toolbarPopoverCloseBehavior, type ToolbarPopoverCloseReason } from './toolbarPopoverState'
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
  const suppressFocusOpen = useRef(false)

  const close = (reason: ToolbarPopoverCloseReason) => {
    const behavior = toolbarPopoverCloseBehavior(reason)
    if (behavior.suppressFocusOpen) suppressFocusOpen.current = true
    setOpen(false)
    if (behavior.restoreFocus) window.setTimeout(() => trigger.current?.focus(), 0)
  }

  useEffect(() => {
    if (!open) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (!shouldCloseToolbarPopoverOnKey(event.key)) return
      event.preventDefault()
      close('escape')
    }
    const closeOnOutsidePointer = (event: PointerEvent) => {
      if (shouldCloseToolbarPopoverForPointer(Boolean(root.current?.contains(event.target as Node)))) close('outside')
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
    if (!next || !root.current?.contains(next)) close('blur')
  }}>
    {announcement && <span className="visually-hidden" role={liveRole} aria-live={liveRole === 'alert' ? 'assertive' : 'polite'}>{announcement}</span>}
    <Button variant="quiet" ref={trigger} type="button" className={`toolbar-popover-trigger${showLabel ? ' toolbar-popover-trigger-labeled' : ''}`} aria-label={label} aria-haspopup="dialog" aria-expanded={open} aria-controls={id} title={title || label} onFocus={event => {
      if (suppressFocusOpen.current) { suppressFocusOpen.current = false; return }
      if (shouldOpenToolbarPopoverOnFocus(event.currentTarget.matches(':focus-visible'))) setOpen(true)
    }} onClick={() => setOpen(true)}>
      {icon || defaultIcons[tone]}
      {showLabel && <span>{label}</span>}
    </Button>
    {open && <div id={id} ref={panel} className="toolbar-popover-panel" role="dialog" aria-label={label} onClick={event => {
      const target = event.target as HTMLElement
      const action = target.closest('button:not(:disabled), a[href]')
      if (!action) return
      close(action.hasAttribute('data-toolbar-popover-close') ? 'explicit' : 'action')
    }}>{children}</div>}
  </span>
}
