import type { ReactNode } from 'react'

export function SettingsTaskSwitcher<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: readonly { value: T; label: string; icon?: ReactNode }[]
  onChange: (value: T) => void
}) {
  return <div className="settings-task-switcher" role="tablist" aria-label="设置任务">
    {options.map(option => <button key={option.value} type="button" role="tab" aria-selected={value === option.value} className={value === option.value ? 'active' : ''} onClick={() => onChange(option.value)}>
      {option.icon}<span>{option.label}</span>
    </button>)}
  </div>
}
