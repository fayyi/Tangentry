import { type ReactNode } from 'react'
import './tabs.css'

export interface Tab {
  id: string
  label: string
  content?: ReactNode
}

interface TabsProps {
  tabs: Tab[]
  activeTab: string
  onTabChange: (tabId: string) => void
  className?: string
}

export function Tabs({ tabs, activeTab, onTabChange, className = '' }: TabsProps) {
  return (
    <div className={['tabs', className].filter(Boolean).join(' ')}>
      <div className="tabs-list" role="tablist">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={activeTab === tab.id}
            aria-controls={`panel-${tab.id}`}
            className={['tab', activeTab === tab.id && 'tab-active'].filter(Boolean).join(' ')}
            onClick={() => onTabChange(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>
      {tabs.map((tab) => (
        <div
          key={tab.id}
          id={`panel-${tab.id}`}
          role="tabpanel"
          hidden={activeTab !== tab.id}
          className="tab-panel"
        >
          {tab.content}
        </div>
      ))}
    </div>
  )
}
