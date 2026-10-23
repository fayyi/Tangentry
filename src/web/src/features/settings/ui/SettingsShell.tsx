import { Menu, X } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Button, Dialog } from '../../../components/ui'

export type SettingsNavItem = {
  id: string
  label: string
  hint: string
  group: string
  icon: ReactNode
}

export function SettingsShell({ items, activeId, onSelect, children }: {
  items: SettingsNavItem[]
  activeId: string
  onSelect: (id: string) => void
  children: ReactNode
}) {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const active = items.find(item => item.id === activeId) ?? items[0]
  const groups = [...new Set(items.map(item => item.group))]

  function select(id: string) {
    setMobileMenuOpen(false)
    onSelect(id)
  }

  return <div className="settings-system">
    <header className="settings-system-header">
      <h1 className="sr-only">设置</h1>
      <nav className="settings-system-top-nav" aria-label="设置目录">
        {items.map(item => <Button variant="quiet" type="button" key={item.id} className={item.id === activeId ? 'active' : ''} aria-current={item.id === activeId ? 'page' : undefined} onClick={() => select(item.id)}>
          <span className="settings-system-nav-icon">{item.icon}</span><span>{item.label}</span>
        </Button>)}
      </nav>
      <Button variant="quiet" type="button" className="settings-mobile-menu-button" aria-label="打开设置目录" onClick={() => setMobileMenuOpen(true)}><Menu size={18} /><span>设置目录</span><strong>{active?.label}</strong></Button>
    </header>
    <div className="settings-system-body">
      <div className="settings-system-main">{children}</div>
    </div>
    <Dialog open={mobileMenuOpen} className="settings-system-mobile-dialog" aria-label="设置目录" onCancel={() => setMobileMenuOpen(false)} onClose={() => setMobileMenuOpen(false)}>
      <div className="settings-system-mobile-dialog-head"><strong>设置目录</strong><Button variant="quiet" type="button" className="settings-back-button" aria-label="关闭设置目录" onClick={() => setMobileMenuOpen(false)}><X size={18} /></Button></div>
      <nav className="settings-system-mobile-nav">{groups.map(group => <div key={group}><span className="settings-system-nav-label">{group}</span>{items.filter(item => item.group === group).map(item => <Button variant="quiet" type="button" key={item.id} className={item.id === activeId ? 'active' : ''} aria-current={item.id === activeId ? 'page' : undefined} onClick={() => select(item.id)}><span className="settings-system-nav-icon">{item.icon}</span><span>{item.label}</span></Button>)}</div>)}</nav>
    </Dialog>
  </div>
}
