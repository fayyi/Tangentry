import type { ReactNode } from 'react'

export function SettingsActionPage({
  title,
  description,
  status,
  actions,
  children,
  className = '',
}: {
  eyebrow: string
  title: string
  description: string
  status?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return <section className={`settings-action-page ${className}`.trim()} aria-labelledby="settings-action-title">
    <header className="settings-action-head">
      <div>
        <h2 id="settings-action-title">{title}</h2>
        <p>{description}</p>
      </div>
      {(status || actions) && <div className="settings-action-head-actions">{status && <span className="settings-inline-status">{status}</span>}{actions}</div>}
    </header>
    <div className="settings-action-body">{children}</div>
  </section>
}

export function SettingsTask({ title, description, children, actions, className = '' }: {
  title?: ReactNode
  description?: ReactNode
  children: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return <section className={`settings-task ${className}`.trim()}>
    {(title || description || actions) && <div className="settings-task-head">{(title || description) && <div>{title && <h3>{title}</h3>}{description && <p>{description}</p>}</div>}{actions && <div className="settings-task-actions">{actions}</div>}</div>}
    <div className="settings-task-content">{children}</div>
  </section>
}

export function SettingsActionBar({ children }: { children: ReactNode }) {
  return <div className="settings-action-bar">{children}</div>
}
