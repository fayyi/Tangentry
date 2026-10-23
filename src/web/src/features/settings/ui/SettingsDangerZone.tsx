import type { ReactNode } from 'react'

export function SettingsDangerZone({ title = '需要确认的操作', children }: { title?: ReactNode; children: ReactNode }) {
  return <aside className="settings-danger-zone"><div><strong>{title}</strong><p>这些操作会改变或移除现有配置。</p></div>{children}</aside>
}
