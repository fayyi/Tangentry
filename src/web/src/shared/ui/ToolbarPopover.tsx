import { AlertCircle, Check, Info } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
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
  const root = useRef<HTMLSpanElement>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (!open) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      setOpen(false)
    }
    const closeOnOutsidePointer = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('keydown', closeOnEscape)
    document.addEventListener('pointerdown', closeOnOutsidePointer)
    return () => {
      document.removeEventListener('keydown', closeOnEscape)
      document.removeEventListener('pointerdown', closeOnOutsidePointer)
    }
  }, [open])

  const liveRole = tone === 'danger' ? 'alert' : 'status'
  return <span ref={root} className={`toolbar-popover toolbar-popover-${tone}`} onBlur={event => {
    const next = event.relatedTarget as Node | null
    if (!next || !root.current?.contains(next)) setOpen(false)
  }}>
    {announcement && <span className="visually-hidden" role={liveRole} aria-live={liveRole === 'alert' ? 'assertive' : 'polite'}>{announcement}</span>}
    <button type="button" className={`toolbar-popover-trigger${showLabel ? ' toolbar-popover-trigger-labeled' : ''}`} aria-label={label} aria-haspopup="dialog" aria-expanded={open} aria-controls={id} title={title || label} onFocus={event => {
      if (event.currentTarget.matches(':focus-visible')) setOpen(true)
    }} onClick={() => setOpen(true)}>
      {icon || defaultIcons[tone]}
      {showLabel && <span>{label}</span>}
    </button>
    {open && <div id={id} className="toolbar-popover-panel" role="dialog" aria-label={label}>{children}</div>}
  </span>
}
