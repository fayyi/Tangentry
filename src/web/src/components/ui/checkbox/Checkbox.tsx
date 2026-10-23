import { forwardRef, type InputHTMLAttributes } from 'react'
import './checkbox.css'

interface CheckboxProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
}

export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(
  ({ label, className = '', ...props }, ref) => {
    const input = <input {...props} ref={ref} type="checkbox" className={['ui-checkbox', className].filter(Boolean).join(' ')}/>
    return label ? <label className="ui-checkbox-wrapper">{input}<span>{label}</span></label> : input
  }
)

Checkbox.displayName = 'Checkbox'
