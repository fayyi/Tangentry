import { useCallback, useState } from 'react'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface ToastMessage {
  id: string
  message: string
  type: ToastType
  duration?: number
}

export function useToast() {
  const [toasts, setToasts] = useState<ToastMessage[]>([])

  const show = useCallback((message: string, type: ToastType = 'info', duration = 3000) => {
    const id = `toast-${Date.now()}-${Math.random()}`
    setToasts(previous => [...previous, { id, message, type, duration }])

    if (duration > 0) {
      window.setTimeout(() => {
        setToasts(previous => previous.filter(toast => toast.id !== id))
      }, duration)
    }

    return id
  }, [])

  const hide = useCallback((id: string) => {
    setToasts(previous => previous.filter(toast => toast.id !== id))
  }, [])

  const success = useCallback((message: string, duration?: number) => show(message, 'success', duration), [show])
  const error = useCallback((message: string, duration?: number) => show(message, 'error', duration), [show])
  const warning = useCallback((message: string, duration?: number) => show(message, 'warning', duration), [show])
  const info = useCallback((message: string, duration?: number) => show(message, 'info', duration), [show])

  return { toasts, show, hide, success, error, warning, info }
}
