import { Button, Input, Select } from '../../components/ui'
import { ArrowDown, ArrowRight, ArrowUpRight, ArrowLeft, BookOpen, CalendarDays, Clock3, Files, Hash, LoaderCircle, Pencil, Plus, RefreshCw, Search, Pin, PinOff, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import { recoverySources } from './draftStorage'
import type { ContentItem, ReadingNote } from '../../shared/api/types'
import { NoteBody } from './NoteBody'
import { notePresentation } from './notePresentation'
import { SubtitleReferences } from './SubtitleReferences'
import { NoteFrame } from './NoteFrame'
import { NotesPanel, type NoteEditRequest } from './NotesPanel'
import { noteSourceLabel } from './source'
import type { useGlobalNotes } from './useGlobalNotes'
import { QuickMemo } from './QuickMemo'
import { NoteExport } from './NoteExport'
import { localDay, dayLabel } from './timeline'
import { formatPosition } from './NotesPanel'
import { SavedNoteViews } from './SavedNoteViews'
import { NoteSourceContext } from './NoteSourceContext'
import type { TaskControls } from './taskList'
import './global-notes.css'

function NoteResult({ note, title, onSelect, onRead, onContext, contextOpen, selected, dateLabel, onTag, onPin, pinPending, onTaskChange, tasksDisabled }: { onContext: (button: HTMLButtonElement) => void; contextOpen: boolean; onPin: () => void; pinPending: boolean; dateLabel?: string; onTag: (tag: string) => void; note: ReadingNote; title: string; onSelect: () => void; onRead: () => void; selected: boolean } & TaskControls) {
  const presentation = notePresentation(note)
  const standalone = note.contentId.startsWith('memo:')
  const readLabel = /^(bilibili|youtube|douyin):/.test(note.contentId) ? '视频阅读' : '内容阅读'
  return <li className={selected ? 'selected' : ''} data-note-id={note.id}>
    {dateLabel && <h2 className="global-note-date">{dateLabel}</h2>}
    <div className="global-note-entry">
    <div className="global-note-source"><h3>{title}</h3><time dateTime={new Date(note.createdAt * 1000).toISOString()} title={new Date(note.createdAt * 1000).toLocaleString('zh-CN')}>{new Date(note.createdAt * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</time><Button variant="quiet" iconOnly type="button" className="icon-button note-pin" aria-label={note.pinned ? '取消固定笔记' : '固定笔记'} title={note.pinned ? '取消固定笔记' : '固定笔记'} aria-pressed={Boolean(note.pinned)} disabled={pinPending} onClick={onPin}>{pinPending ? <LoaderCircle size={16} className="spin"/> : note.pinned ? <PinOff size={16}/> : <Pin size={16}/>}</Button></div>
    {presentation.quote && <Button variant="quiet" type="button" className="global-note-excerpt" title="回到摘录来源" aria-label={`回到摘录来源：${title}`} onClick={onRead}>{presentation.quote}</Button>}
    <SubtitleReferences references={note.subtitleReferences} contentId={note.contentId}/>
    <NoteFrame note={note}/>
    {presentation.body && <NoteBody id={`global-${note.id}`} body={presentation.body} active={false} bodyClassName="global-note-body" tasksDisabled={tasksDisabled} onTaskChange={(offset, checked) => onTaskChange?.(offset + note.body.length - presentation.body.length, checked)}/>}
    {Boolean(note.tags?.length) && <div className="note-tags" aria-label="笔记标签">{note.tags!.map(tag => <Button variant="quiet" type="button" key={tag} onClick={() => onTag(tag)} aria-label={`筛选笔记标签 ${tag}`}>#{tag}</Button>)}</div>}
    <div className="global-note-actions"><span>{note.positionSeconds !== null && !note.pdfAnchor && !note.textAnchor && <Clock3 size={13} aria-hidden="true"/>}{note.pdfAnchor ? `PDF · 第 ${note.pdfAnchor.page} 页` : note.textAnchor ? '原文批注' : note.positionSeconds === null ? standalone ? '随手记' : '未关联时间' : `播放位置 ${formatPosition(note.positionSeconds)}`}</span><Button variant="quiet" iconOnly type="button" className="icon-button" title={standalone ? '编辑笔记' : '编辑来源笔记'} aria-pressed={selected} onClick={onSelect} aria-label={`编辑来源笔记：${title}，${note.body.slice(0, 24)}`}><Pencil size={16}/></Button>{!standalone && <><Button variant="quiet" iconOnly type="button" className="icon-button" title="查看来源上下文" aria-label={`查看来源上下文：${title}`} aria-expanded={contextOpen} onClick={event => onContext(event.currentTarget)}><BookOpen size={17}/></Button><Button variant="quiet" iconOnly type="button" className="icon-button" title={readLabel} data-content-id={`note:${note.id}`} onClick={onRead} aria-label={`${readLabel}：${title}，${note.body.slice(0, 24)}`}><ArrowUpRight size={17}/></Button></>}</div>
    </div>
  </li>
}

export function GlobalNotesPage({ browser, savedItems, onRead }: { browser: ReturnType<typeof useGlobalNotes>; savedItems: ContentItem[]; onRead: (note: ReadingNote) => void }) {
  const [panel, setPanel] = useState<'browse' | 'write'>('browse')
  const [editRequest, setEditRequest] = useState<NoteEditRequest | null>(null)
  const editToken = useRef(0)
  const [contextNote, setContextNote] = useState<ReadingNote | null>(null)
  const contextTrigger = useRef<HTMLButtonElement | null>(null)
  const openContext = (note: ReadingNote, button: HTMLButtonElement) => {
    contextTrigger.current = button
    setContextNote(note)
  }
  const closeContext = () => {
    setContextNote(null)
    requestAnimationFrame(() => {
      const button = contextTrigger.current
      if (button?.isConnected) button.focus({ preventScroll: true })
      else document.querySelector<HTMLInputElement>('.global-notes-search input')?.focus({ preventScroll: true })
    })
  }
  const [draftSources, setDraftSources] = useState(recoverySources)
  useEffect(() => {
    const refresh = () => setDraftSources(recoverySources())
    window.addEventListener('storage', refresh)
    window.addEventListener('tangentry-drafts-changed', refresh)
    window.addEventListener('focus', refresh)
    refresh()
    return () => { window.removeEventListener('storage', refresh); window.removeEventListener('focus', refresh); window.removeEventListener('tangentry-drafts-changed', refresh) }
  }, [browser.selectedContentId, browser.notes])
  const titles = new Map(savedItems.map(item => [item.id, item.title]))
  for (const note of browser.notes) if (!titles.has(note.contentId) && note.source?.title) titles.set(note.contentId, note.source.title)
  const select = (id: string, noteId?: string) => {
    setContextNote(null)
    setEditRequest(noteId ? { id: noteId, token: ++editToken.current } : null)
    browser.setSelectedContentId(id)
    setPanel('write')
    requestAnimationFrame(() => { const editor = document.getElementById('note-body'); editor?.focus({ preventScroll: true }); editor?.scrollIntoView({ block: 'center' }) })
  }
  const browse = () => {
    setPanel('browse')
    requestAnimationFrame(() => document.querySelector<HTMLInputElement>('.global-notes-search input[type="search"]')?.focus({ preventScroll: true }))
  }
  const compose = () => {
    setContextNote(null)
    setEditRequest(null)
    browser.setSelectedContentId(null)
    setPanel('write')
    requestAnimationFrame(() => document.getElementById('quick-memo-body')?.focus({ preventScroll: true }))
  }
  const sourceTitle = (id: string) => titles.get(id) ?? noteSourceLabel(id)
  const filtered = Boolean(browser.query || browser.filters.tag || browser.filters.from || browser.filters.to || browser.filters.pinnedOnly)
  const today = localDay(Math.floor(Date.now() / 1000))
  const daysAgo = (days: number) => localDay(Math.floor(Date.now() / 1000) - days * 86400)
  const datePresets = [['全部时间', '', ''], ['今天', today, today], ['近 7 天', daysAgo(6), today], ['近 30 天', daysAgo(29), today]] as const
  const tagTree = browser.tags.map(item => ({ ...item, depth: item.tag.split('/').length - 1, name: item.tag.split('/').at(-1) ?? item.tag }))
  return <div className="notebook" data-panel={panel} data-context={contextNote ? 'open' : undefined}>
    <aside className="notebook-sidebar" aria-label="笔记筛选">
      <div className="notebook-sidebar-head"><h1>笔记</h1><Button variant="quiet" iconOnly type="button" className="icon-button notebook-compose" title="新建随手记" aria-label="新建随手记" onClick={compose}><Plus size={17}/></Button></div>
      <form className="global-notes-search" role="search" onSubmit={event => { event.preventDefault(); browser.search() }}>
        <label><Search size={15} aria-hidden="true" /><Input type="search" aria-label="搜索全部笔记正文" value={browser.draftQuery} maxLength={200} placeholder="搜索笔记" onChange={event => browser.setDraftQuery(event.target.value)} /></label>
        <Button variant="quiet" iconOnly className="icon-button" aria-label="搜索笔记" title="搜索笔记" disabled={browser.loading || Array.from(browser.draftQuery.trim()).length > 200}><ArrowRight size={15}/></Button>
      </form>
      <div className="notebook-scope" role="group" aria-label="笔记范围">
        <Button variant="quiet" type="button" aria-pressed={!browser.filters.pinnedOnly} disabled={browser.loading} onClick={() => browser.filterPinned(false)}><Files size={15} aria-hidden="true"/>全部</Button>
        <Button variant="quiet" type="button" aria-pressed={Boolean(browser.filters.pinnedOnly)} disabled={browser.loading} onClick={() => browser.filterPinned(true)}><Pin size={15} aria-hidden="true"/>固定</Button>
      </div>
      <section className="notebook-facet" aria-labelledby="notebook-tags-title">
        <h2 id="notebook-tags-title"><Hash size={13} aria-hidden="true"/>标签</h2>
        {tagTree.length > 0 ? <ul className="notebook-tag-tree">{tagTree.map(item => <li key={item.tag} style={{ '--depth': item.depth } as CSSProperties}><Button variant="quiet" type="button" aria-pressed={browser.filters.tag === item.tag} disabled={browser.loading} title={`#${item.tag}`} onClick={() => browser.filterTag(browser.filters.tag === item.tag ? '' : item.tag)}><span>{item.name}</span><small>{item.count}</small></Button></li>)}</ul> : <p className="notebook-facet-empty">保存笔记时添加标签，就能在这里按主题浏览。</p>}
        <label className="notebook-tag-select">选择标签<Select aria-label="筛选笔记标签" value={browser.draftTag} onChange={event => browser.setDraftTag(event.target.value)}><option value="">全部标签</option>{browser.draftTag && !browser.tags.some(item => item.tag === browser.draftTag) && <option value={browser.draftTag}>#{browser.draftTag}</option>}{browser.tags.map(item => <option key={item.tag} value={item.tag}>#{item.tag} ({item.count})</option>)}</Select></label>
      </section>
      <section className="notebook-facet" aria-labelledby="notebook-dates-title">
        <h2 id="notebook-dates-title"><CalendarDays size={13} aria-hidden="true"/>时间</h2>
        <div className="notebook-date-presets">{datePresets.map(([label, from, to]) => <Button variant="quiet" key={label} type="button" aria-pressed={browser.fromDate === from && browser.toDate === to} disabled={browser.loading} onClick={() => browser.filterDates(from, to)}>{label}</Button>)}</div>
        <div className="global-note-filters"><label>开始<Input type="date" aria-label="笔记开始日期" min="1970-01-01" max="9998-12-31" value={browser.fromDate} onChange={event => browser.setFromDate(event.target.value)} /></label><label>结束<Input type="date" aria-label="笔记结束日期" min="1970-01-01" max="9998-12-31" value={browser.toDate} onChange={event => browser.setToDate(event.target.value)} /></label></div>
      </section>
      <SavedNoteViews selection={{ query: browser.query, tag: browser.filters.tag, pinnedOnly: Boolean(browser.filters.pinnedOnly), fromDate: browser.filters.from ? localDay(browser.filters.from) : '', toDate: browser.filters.to ? localDay(browser.filters.to - 1) : '' }} canSave={browser.hasResult && !browser.loading && !browser.error} onApply={browser.applyView}/>
      {draftSources.length > 0 && <details className="note-recovery global-draft-sources"><summary>未完成编辑 · {draftSources.length}</summary>{draftSources.map(id => <div key={id}><Button variant="secondary" className="button button-secondary" onClick={() => select(id)} aria-label={`继续来源草稿：${sourceTitle(id)}`}>{sourceTitle(id)}</Button></div>)}</details>}
      {browser.tagsError && <p className="notes-hint" role="status">{browser.tagsError}</p>}
    </aside>
    <div className="notebook-toolbar" data-panel={panel}><Button variant="secondary" type="button" className="button button-secondary notebook-browse" onClick={browse}><ArrowLeft size={16}/>返回笔记列表</Button>{browser.selectedContentId && panel === 'browse' && <Button variant="secondary" type="button" className="button button-secondary notebook-resume" onClick={() => select(browser.selectedContentId!)}>继续编辑</Button>}</div>
    <section className="notebook-browser" aria-label="笔记浏览">
    {browser.pinError && <p role="alert">{browser.pinError}</p>}
    {browser.taskError && <p role="alert">{browser.taskError}</p>}
    {browser.error && <div className="error-panel" role="alert"><div><p>{browser.error}</p>{browser.hasResult && <small>下方保留上次成功结果{browser.query ? `：${browser.query}` : '：全部笔记'}。</small>}</div><Button variant="secondary" className="button button-secondary" disabled={browser.loading} onClick={browser.reload}>重试笔记搜索</Button></div>}
    <div className="notebook-results-toolbar">
    <div className="global-notes-summary"><span>{browser.filters.pinnedOnly ? '固定笔记' : '全部笔记'}{browser.query && ` · 匹配“${browser.query}”`}{browser.filters.tag && ` · #${browser.filters.tag}`}{(browser.filters.from || browser.filters.to) ? ' · 日期已筛选' : ''} · 已加载 {browser.notes.length} 条</span>{(filtered || browser.draftQuery || browser.draftTag || browser.fromDate || browser.toDate) && <Button variant="quiet" iconOnly type="button" className="icon-button" title="清除笔记搜索" aria-label="清除笔记搜索" disabled={browser.loading} onClick={browser.clear}><X size={15}/></Button>}{browser.loading && <><span role="status"><LoaderCircle size={15} className="spin" />正在读取笔记…</span><Button variant="secondary" type="button" className="button button-secondary" onClick={browser.cancel}>取消笔记读取</Button></>}</div>
    <div className="notebook-results-actions"><Button variant="quiet" iconOnly type="button" className="icon-button" aria-label="刷新全部笔记" title="刷新全部笔记" disabled={browser.loading} onClick={browser.reload}><RefreshCw size={15} /></Button><NoteExport compact selection={{q: browser.query, ...browser.filters}} label={filtered ? '导出筛选笔记' : '导出全部笔记'} disabled={!browser.hasResult || browser.loading || browser.notes.length === 0}/></div>
    </div>
    <div className="global-notes-workspace">
      <div>{browser.notes.length > 0 && <ol className="global-note-list" aria-label="全部笔记结果">{browser.notes.map((note, index) => <NoteResult onContext={button => openContext(note, button)} contextOpen={contextNote?.id === note.id} onPin={() => void browser.togglePin(note)} pinPending={browser.pinPending.has(note.id)} tasksDisabled={browser.loading || Boolean(browser.error) || browser.taskPending.has(note.id) || browser.pinPending.has(note.id) || (browser.selectedContentId === note.contentId && editRequest?.id === note.id)} onTaskChange={(offset, checked) => { void browser.toggleTask(note, offset, checked) }} dateLabel={index === 0 || localDay(note.createdAt) !== localDay(browser.notes[index - 1].createdAt) ? dayLabel(note.createdAt) : undefined} onTag={browser.filterTag} key={note.id} note={note} title={sourceTitle(note.contentId)} selected={browser.selectedContentId === note.contentId && editRequest?.id === note.id} onSelect={() => select(note.contentId, note.id)} onRead={() => onRead(note)} />)}</ol>}
        {!browser.loading && !browser.error && browser.notes.length === 0 && <section className="empty-state"><h2>{browser.query || browser.filters.tag || browser.filters.from || browser.filters.to ? '没有找到匹配的笔记' : browser.filters.pinnedOnly ? '还没有固定笔记' : '把阅读中的想法留在这里'}</h2><p>{browser.query || browser.filters.tag || browser.filters.from || browser.filters.to ? '试试其他关键词、标签或日期。' : browser.filters.pinnedOnly ? '固定需要回看的笔记，留待下次继续。' : '写一条随手记，或在看视频时截屏记录。'}</p></section>}
        {browser.nextCursor && <div className="load-more"><Button variant="secondary" className="button button-secondary" disabled={browser.loading || Boolean(browser.error)} onClick={browser.more}><ArrowDown size={15} />继续读取笔记</Button></div>}
      </div>
    </div>
    </section>
    <div className="notebook-writing">
      <div hidden={Boolean(browser.selectedContentId)}><QuickMemo /></div>
      {browser.selectedContentId && <aside className="global-note-editor" aria-label="来源笔记编辑"><div className="global-editor-heading"><strong>{sourceTitle(browser.selectedContentId)}</strong><Button variant="quiet" iconOnly type="button" className="icon-button" title="收起编辑器" aria-label="收起编辑器" onClick={() => { browser.setSelectedContentId(null); setEditRequest(null); browse() }}><X size={17}/></Button></div><NotesPanel key={browser.selectedContentId} contentId={browser.selectedContentId} editRequest={editRequest} target={null} /></aside>}
    </div>
    <NoteSourceContext note={contextNote} title={contextNote ? sourceTitle(contextNote.contentId) : ''} item={savedItems.find(item => item.id === contextNote?.contentId)} onClose={closeContext} onRead={onRead}/>
  </div>
}
