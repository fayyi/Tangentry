import { ArrowLeft, ArrowRight, BookOpen, Bookmark, Clock3, Compass, Download, Headphones, Menu, Moon, PanelLeftClose, PanelLeftOpen, Plus, Rss, Search, Settings, StickyNote, Sun, X, ChevronUp, ChevronDown, FileText, Play, Image, Filter, Newspaper, Circle } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { SavedView } from '../shared/api/types'
import type { View } from './navigation'
import { viewNames } from './navigation'
import { useThemeSwitch } from './useThemeSwitch'
import { BrandMark } from '../shared/ui/BrandMark'
import { useHistoryGestures, useHistoryPosition } from './useHistoryGestures'

// Navigation follows what the user is doing: finding new things to read or
// listen to, and returning to what is already kept on this machine.
const groups = [
  { label: '浏览', items: [
    { view: 'discovery', label: '发现', Icon: Compass },
    { view: 'podcasts', label: '播客', Icon: Headphones },
    { view: 'subscriptions', label: '订阅', Icon: Rss },
  ] },
  { label: '资料库', items: [
    { view: 'library', label: '收藏', Icon: Bookmark },
    { view: 'history', label: '历史', Icon: Clock3 },
    { view: 'notes', label: '笔记', Icon: StickyNote },
    { view: 'downloads', label: '下载', Icon: Download },
  ] },
] as const
const destinations: readonly { view: View; label: string; Icon: typeof Compass }[] = groups.flatMap(group => [...group.items])
const savedViewIcons = { 'file-text': FileText, play: Play, headphones: Headphones, image: Image, bookmark: Bookmark, filter: Filter, newspaper: Newspaper, circle: Circle } as const
const quick = ['discovery', 'podcasts', 'subscriptions', 'library', 'downloads'] as const
const groupOf = (view: View) => view === 'tools' ? '应用' : view === 'reader' || view === 'search' ? '' : groups.find(group => group.items.some(item => item.view === view))?.label ?? (view === 'bilibili' ? '浏览' : '')

export function ExperienceShell({ view, onNavigate, onNavigateSavedView, savedViews, onReorderSavedViews, onSearchQuery, onHome, activeDownloads, savedCount, onAdd, discoveryQuery, onDiscoveryQuery, resume, onResume, onDismissResume, children }: {
  view: View
  onNavigate: (view: View) => void
  onSearchQuery: (query: string) => void
  onNavigateSavedView: (id: string) => void
  savedViews: SavedView[]
  onReorderSavedViews: (ids: string[]) => void
  onHome: (animate: boolean) => void
  activeDownloads: number
  savedCount: number
  onAdd: () => void
  discoveryQuery: string
  onDiscoveryQuery: (query: string) => void
  resume: { title: string } | null
  onResume: () => void
  onDismissResume: () => void
  children: ReactNode
}) {
  const swipe = useHistoryGestures()
  const position = useHistoryPosition()
  const [menuOpen, setMenuOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem('tangentry-sidebar-collapsed') !== 'false' } catch { return true }
  })
  const menu = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const { dark, switchTheme } = useThemeSwitch()
  useEffect(() => {
    try { localStorage.setItem('tangentry-sidebar-collapsed', String(collapsed)) } catch { /* Navigation remains usable without storage. */ }
  }, [collapsed])
  useEffect(() => {
    const keyboard = () => { document.documentElement.dataset.input = 'keyboard' }
    const pointer = () => { document.documentElement.dataset.input = 'pointer' }
    const resize = () => { if (!matchMedia('(max-width: 960px)').matches) setMenuOpen(false) }
    window.addEventListener('keydown', keyboard)
    window.addEventListener('pointerdown', pointer)
    window.addEventListener('resize', resize)
    return () => {
      window.removeEventListener('keydown', keyboard)
      window.removeEventListener('pointerdown', pointer)
      window.removeEventListener('resize', resize)
    }
  }, [])
  useEffect(() => {
    const node = menu.current
    if (!node || !menuOpen) return
    node.showModal()
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      node.close()
      document.body.style.overflow = previous
      trigger.current?.focus({ preventScroll: true })
    }
  }, [menuOpen])
  const go = (next: View) => { setMenuOpen(false); onNavigate(next) }
  const badge = (next: View) => next === 'library' && savedCount > 0 ? savedCount : next === 'downloads' && activeDownloads > 0 ? activeDownloads : 0
  const navItems = (mobile = false) => <nav className={mobile ? 'experience-mobile-links' : 'desktop-nav experience-links'} aria-label={mobile ? '移动导航' : '主导航'}>
    {groups.map(group => <div className="experience-nav-group" key={group.label}>
      <span className="experience-nav-label" aria-hidden="true">{group.label}</span>
      {group.items.map(({ view: next, label, Icon }) => <button key={next} type="button" className={view === next ? 'active' : ''} aria-label={label} title={label} aria-current={view === next ? 'page' : undefined} onClick={() => go(next)}>
        <Icon size={18} strokeWidth={1.8} aria-hidden="true"/><span>{label}</span>{badge(next) > 0 && <small className={next === 'downloads' ? 'is-live' : undefined} aria-hidden="true">{badge(next)}</small>}
      </button>)}
      {group.label === '资料库' && <div className="saved-view-nav-list" role="group" aria-label="保存视图">{savedViews.map((savedView,index)=><div className="saved-view-nav-row" key={savedView.id}>
        {(() => { const ViewIcon = savedViewIcons[savedView.icon as keyof typeof savedViewIcons] ?? Filter; return <button type="button" className={view === 'subscriptions' && new URLSearchParams(location.search).get('view') === savedView.id ? 'active' : ''} aria-label={`${savedView.name}，${savedView.count} 条`} title={savedView.name} onClick={()=>{setMenuOpen(false);onNavigateSavedView(savedView.id)}}><span className={`saved-view-mark ${savedView.color}`}><ViewIcon size={13}/></span><span>{savedView.name}</span><small>{savedView.count}</small></button> })()}
        {!collapsed && <span className="saved-view-order"><button type="button" aria-label={`上移 ${savedView.name}`} title="上移" disabled={index===0} onClick={()=>{const ids=savedViews.map(item=>item.id);[ids[index-1],ids[index]]=[ids[index],ids[index-1]];onReorderSavedViews(ids)}}><ChevronUp size={12}/></button><button type="button" aria-label={`下移 ${savedView.name}`} title="下移" disabled={index===savedViews.length-1} onClick={()=>{const ids=savedViews.map(item=>item.id);[ids[index],ids[index+1]]=[ids[index+1],ids[index]];onReorderSavedViews(ids)}}><ChevronDown size={12}/></button></span>}
      </div>)}</div>}
    </div>)}
    <div className="experience-nav-group experience-nav-utility">
      <button type="button" className={`experience-settings-link ${view === 'tools' ? 'active' : ''}`} aria-label="工具设置" title="设置" aria-current={view === 'tools' ? 'page' : undefined} onClick={() => go('tools')}><Settings size={18} strokeWidth={1.8} aria-hidden="true"/><span>设置</span></button>
    </div>
  </nav>
  const section = groupOf(view)
  return <div className={`experience ${view === 'reader' ? 'experience-reading' : ''}`} data-view={view} data-sidebar-collapsed={collapsed}>
    <a href="#main" className="skip-link">跳到内容</a>
    <aside id="desktop-sidebar" className="experience-sidebar" aria-label="个人内容空间">
      <div className="experience-sidebar-head">
        <button type="button" className="brand-button" aria-label="返回首页" title="返回发现页顶部" onClick={event => onHome(event.detail !== 0 && !matchMedia('(prefers-reduced-motion: reduce)').matches)}><BrandMark/><span className="brand-name">Tangentry</span></button>
      </div>
      {navItems()}
      <div className="experience-sidebar-footer">
        <span className="experience-local" title="内容与笔记保存在本机"><span className="local-dot" aria-hidden="true"/><span>本机资料库</span></span>
        <button type="button" className="experience-sidebar-toggle" aria-label={collapsed ? '展开侧栏' : '收起侧栏'} title={collapsed ? '展开侧栏' : '收起侧栏'} aria-controls="desktop-sidebar" aria-expanded={!collapsed} onClick={() => setCollapsed(value => !value)}>{collapsed ? <PanelLeftOpen size={18}/> : <PanelLeftClose size={18}/>}</button>
      </div>
    </aside>
    <header className="experience-topbar top-navigation">
      <div className="experience-location">
        <button ref={trigger} type="button" className="icon-button experience-menu" aria-label="菜单" title="打开导航" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)}><Menu size={20}/></button>
        <span className="experience-mobile-brand" aria-hidden="true"><BrandMark/></span>
        <span className="experience-history" role="group" aria-label="页面历史">
          <button type="button" className="icon-button" aria-label="后退" title="后退（⌘[ 或向右轻扫）" disabled={!position.back} onClick={() => history.back()}><ArrowLeft size={17}/></button>
          <button type="button" className="icon-button" aria-label="前进" title="前进（⌘] 或向左轻扫）" disabled={!position.forward} onClick={() => history.forward()}><ArrowRight size={17}/></button>
        </span>
        <span className="experience-location-title">{section && <span className="experience-location-section">{section}</span>}<strong>{viewNames[view]}</strong></span>
      </div>
      {view === 'discovery' && <div className="discovery-search" role="search"><Search size={16} aria-hidden="true"/><input type="search" aria-label="搜索发现内容" placeholder="搜索本机与各平台" value={discoveryQuery} onChange={event => onDiscoveryQuery(event.target.value)} onFocus={() => onSearchQuery(discoveryQuery)}/>{discoveryQuery && <button type="button" className="icon-button" aria-label="清除发现搜索" title="清除搜索" onClick={() => onDiscoveryQuery('')}><X size={15}/></button>}</div>}
      <div className="experience-header-actions">
        <button type="button" className="experience-add" aria-label="添加内容" title="粘贴链接或导入 PDF" onClick={onAdd}><Plus size={17} aria-hidden="true"/><span>添加内容</span></button>
        <button type="button" className="icon-button theme-switch" aria-label={dark ? '切换浅色模式' : '切换深色模式'} title={dark ? '切换浅色模式' : '切换深色模式'} onClick={switchTheme}>{dark ? <Sun size={18}/> : <Moon size={18}/>}</button>
      </div>
    </header>
    <dialog ref={menu} className="experience-drawer" aria-label="导航菜单" onCancel={() => setMenuOpen(false)} onClose={() => setMenuOpen(false)} onClick={event => {
      if (event.target === event.currentTarget) {
        const rect = event.currentTarget.getBoundingClientRect()
        if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) setMenuOpen(false)
      }
    }}><div className="experience-drawer-heading"><span className="experience-drawer-brand"><BrandMark/><strong>Tangentry</strong></span><button type="button" className="icon-button" aria-label="关闭菜单" onClick={() => setMenuOpen(false)}><X size={20}/></button></div>{navItems(true)}</dialog>
    <div className="experience-workspace">{children}</div>
    {resume && <aside className="experience-resume" aria-label="继续阅读">
      <button type="button" className="experience-resume-open" title={resume.title} onClick={onResume}><BookOpen size={15} aria-hidden="true"/><span>回到</span><strong>{resume.title}</strong></button>
      <button type="button" className="icon-button" aria-label="不再显示继续阅读" title="关闭" onClick={onDismissResume}><X size={14}/></button>
    </aside>}
    {swipe !== 0 && <div className="experience-swipe-hint" data-direction={swipe < 0 ? 'back' : 'forward'} aria-hidden="true">{swipe < 0 ? <ArrowLeft size={20}/> : <ArrowRight size={20}/>}</div>}
    <nav className="bottom-navigation" aria-label="快捷导航">{quick.map(next => {
      const { label, Icon } = destinations.find(item => item.view === next)!
      return <button key={next} type="button" className={view === next ? 'active' : ''} aria-current={view === next ? 'page' : undefined} onClick={() => go(next)}><Icon size={20} strokeWidth={1.8}/><span>{label}</span>{next === 'downloads' && activeDownloads > 0 && <small>{activeDownloads}</small>}</button>
    })}</nav>
  </div>
}
