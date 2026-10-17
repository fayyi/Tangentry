import type { HTMLAttributes, ReactNode } from 'react'

// Every top-level page opens the same way: what this place is, the state that
// matters right now, and the one or two actions that start work here.
export function PageHeader({ title, summary, actions, className = '', children, ...props }: Omit<HTMLAttributes<HTMLElement>, 'title'> & { title: ReactNode; summary?: ReactNode; actions?: ReactNode }) {
  return <header {...props} className={`page-heading ${className}`.trim()}>
    <div className="page-heading-copy"><h1>{title}</h1>{summary && <p>{summary}</p>}</div>
    {actions && <div className="page-heading-actions">{actions}</div>}
    {children}
  </header>
}
