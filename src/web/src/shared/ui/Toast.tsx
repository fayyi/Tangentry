import { AlertTriangle, Check, Info, X } from 'lucide-react'
import { useEffect } from 'react'

type ToastType = 'success' | 'error' | 'warning' | 'info'

interface ToastProps {
  message: string
  type?: ToastType
  duration?: number
  visible: boolean
  onClose: () => void
}

const icons = {
  success: Check,
  error: X,
  warning: AlertTriangle,
  info: Info,
} as const

export function Toast({ message, type = 'info', duration = 3000, visible, onClose }: ToastProps) {
  useEffect(() => {
    if (!visible || duration <= 0) return
    const timer = window.setTimeout(onClose, duration)
    return () => window.clearTimeout(timer)
  }, [visible, duration, onClose])

  const Icon = icons[type]
  return <div className={`toast ${type} ${visible ? 'visible' : ''}`} role="alert">
    <Icon size={18} aria-hidden="true" />
    <span>{message}</span>
  </div>
}
