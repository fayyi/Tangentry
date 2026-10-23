import { forwardRef, type SelectHTMLAttributes } from 'react'
import './select.css'

interface SelectOption {
  value: string
  label: string
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  options?: SelectOption[]
  placeholder?: string
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ options = [], placeholder, className = '', children, ...props }, ref) => {
    return (
      <select
        ref={ref}
        className={['ui-select', className].filter(Boolean).join(' ')}
        {...props}
      >
        {placeholder && (
          <option value="" disabled>
            {placeholder}
          </option>
        )}
        {children}
        {options.map(({ value, label }) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
    )
  }
)

Select.displayName = 'Select'
