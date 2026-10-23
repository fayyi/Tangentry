import { forwardRef, useId, type InputHTMLAttributes, type ReactNode } from 'react'
import './input.css'

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  icon?: ReactNode
  error?: string
  hint?: string
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ icon, error, hint, className = '', ...props }, ref) => {
    const id = useId()
    const messageId = `${id}-message`
    const describedBy = [props['aria-describedby'], (error || hint) && messageId].filter(Boolean).join(' ') || undefined
    const input = <input {...props} ref={ref} className={['ui-input', error && 'ui-input-error', className].filter(Boolean).join(' ')} aria-invalid={error ? true : props['aria-invalid']} aria-describedby={describedBy}/>

    if (!icon && !error && !hint) return input
    return <span className="ui-input-wrapper">
      {icon ? <span className="ui-input-with-icon"><span className="ui-input-icon" aria-hidden="true">{icon}</span>{input}</span> : input}
      {error && <span id={messageId} className="ui-input-message ui-input-error-message" role="alert">{error}</span>}
      {hint && !error && <span id={messageId} className="ui-input-message">{hint}</span>}
    </span>
  }
)

Input.displayName = 'Input'
