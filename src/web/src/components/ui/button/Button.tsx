import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import './button.css'

export type ButtonVariant = 'primary' | 'secondary' | 'accent' | 'quiet' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  isLoading?: boolean
  iconOnly?: boolean
  icon?: ReactNode
  children?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = 'secondary', size = 'md', isLoading = false, iconOnly = false, icon, className = '', disabled, children, ...props }, ref) => (
    <button
      {...props}
      ref={ref}
      className={['ui-button', `ui-button-${variant}`, `ui-button-${size}`, iconOnly && 'ui-button-icon', className].filter(Boolean).join(' ')}
      disabled={isLoading || disabled}
      aria-busy={isLoading || props['aria-busy']}
    >
      {isLoading && <span className="ui-button-loader" aria-hidden="true"/>}
      {icon && <span className="ui-button-glyph" aria-hidden="true">{icon}</span>}
      {children}
    </button>
  )
)

Button.displayName = 'Button'
