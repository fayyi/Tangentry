import { ChevronRight, Eye, Languages, Mic, HardDrive, UserRound } from 'lucide-react'
import { replacePath } from '../../shared/browser/historyStack'
import { PageHeader } from '../../shared/ui/PageHeader'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { PlatformCookies } from './PlatformCookies'
import { YouTubeRecommendations } from './YouTubeRecommendations'
import { AISettings } from '../ai/AISettings'
import { TranscriptionSettings } from './TranscriptionSettings'
import type { useDownloads } from '../downloads/useDownloads'
import { GalleryLayoutSettings } from './GalleryLayoutSettings'
import type { GalleryColumns } from './useGalleryLayout'
import type { Feed } from '../../shared/api/types'
import { StorageSettings } from './StorageSettings'

type SettingsSection = 'display' | 'accounts' | 'translation' | 'transcription' | 'storage'
function sectionFromURL(): SettingsSection {
  const hash = window.location.hash.slice(1)
  return ['display', 'accounts', 'translation', 'transcription', 'storage'].includes(hash) ? hash as SettingsSection : 'display'
}

export function ToolsPage({ downloads, onCookieChanged, onYouTubeFeed, onDiscover }: {
  downloads: ReturnType<typeof useDownloads>
  columns: GalleryColumns
  onColumns: (columns: GalleryColumns) => void
  onCookieChanged: (platform: string) => void
  onYouTubeFeed: (feed: Feed) => void
  onDiscover: () => void
}) {
  const [section, setSection] = useState<SettingsSection>(sectionFromURL)
  const [visited, setVisited] = useState(() => new Set<SettingsSection>([sectionFromURL()]))
  const categories = useRef<HTMLElement>(null)
  useLayoutEffect(() => {
    const reveal = () => {
      const node = categories.current, selected = node?.querySelector<HTMLElement>('[aria-current="page"]')
      if (!node || !selected || !matchMedia('(max-width: 760px)').matches) return
      const container = node.getBoundingClientRect(), target = selected.getBoundingClientRect()
      if (target.left < container.left) node.scrollLeft -= container.left - target.left
      else if (target.right > container.right) node.scrollLeft += target.right - container.right
    }
    reveal()
    window.addEventListener('resize', reveal)
    return () => window.removeEventListener('resize', reveal)
  }, [section])
  function selectSection(next: SettingsSection) {
    setSection(next)
    setVisited(current => new Set([...current, next]))
    replacePath('/tools#' + next)
  }
  useEffect(() => {
    const update = () => {
      const next = sectionFromURL()
      setSection(next)
      setVisited(current => new Set([...current, next]))
    }
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  const [youtubeCookieRevision, setYouTubeCookieRevision] = useState(0)
  const [modelRevision, setModelRevision] = useState(0)
  function cookiesChanged(platform: string) {
    if (platform === 'youtube') setYouTubeCookieRevision(revision => revision + 1)
    onCookieChanged(platform)
    void downloads.reload()
  }
  const sections = [
    { id: 'display' as const, label: '显示', hint: '收件箱每页条数', Icon: Eye },
    { id: 'accounts' as const, label: '账号与来源', hint: '登录与内容推荐', Icon: UserRound },
    { id: 'translation' as const, label: '翻译与摘要', hint: 'AI 翻译与文章摘要', Icon: Languages },
    { id: 'transcription' as const, label: '字幕转录', hint: '本机或云端识别', Icon: Mic },
    { id: 'storage' as const, label: '下载与存储', hint: '工具与文件位置', Icon: HardDrive },
  ]
  return <>
    <PageHeader className="settings-page-heading" title="设置" summary="账号与推荐来源、翻译和转录服务、下载与文件位置。更改只保存在这台设备。"/>
    {downloads.error && <p role="alert" className="error-panel">{downloads.error}</p>}
    <div className="settings-layout">
      <nav ref={categories} className="settings-nav" aria-label="设置类别">{sections.map(({ id, label, hint, Icon }) => <button type="button" key={id} className={section === id ? 'active' : ''} aria-current={section === id ? 'page' : undefined} aria-controls={`settings-${id}`} onClick={() => selectSection(id)}><span className="settings-category-icon" data-category={id}><Icon size={17} strokeWidth={1.7} aria-hidden="true"/></span><span><strong>{label}</strong><small>{hint}</small></span><ChevronRight size={14} className="settings-nav-chevron" aria-hidden="true"/></button>)}</nav>
      <div className="settings-workspace">
      <div id="settings-display" className="settings-block" hidden={section !== 'display'}>{visited.has('display') && <GalleryLayoutSettings/>}</div>
      <div id="settings-storage" className="settings-block" hidden={section !== 'storage'}>{visited.has('storage') && <StorageSettings downloads={downloads} onModelDirectoryChanged={() => setModelRevision(value => value + 1)}/>}</div>
      <div id="settings-accounts" className="settings-block settings-block-wide" hidden={section !== 'accounts'}>{visited.has('accounts') && <><PlatformCookies onChanged={cookiesChanged} /><YouTubeRecommendations key={youtubeCookieRevision} tools={downloads.tools} onFeed={onYouTubeFeed} onDiscover={onDiscover}/></>}</div>
      <div id="settings-translation" className="settings-block settings-block-wide" hidden={section !== 'translation'}>{visited.has('translation') && <AISettings/>}</div>
      <div id="settings-transcription" className="settings-block settings-block-wide" hidden={section !== 'transcription'}>{visited.has('transcription') && <TranscriptionSettings key={modelRevision}/>}</div>
      </div>
    </div>
  </>
}
