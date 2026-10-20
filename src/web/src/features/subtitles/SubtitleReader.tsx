import type { SubtitleAssistance } from '../../shared/ui/subtitleAssistance'
import { ArrowDownToLine, Captions, ExternalLink, LoaderCircle, Search, X, HardDrive, Save, ChevronLeft, ChevronRight, ChevronDown, ChevronUp, SlidersHorizontal, Repeat2, StickyNote, Quote } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { ContentItem, SubtitleAvailability, SubtitleCue, SubtitleTrack, SubtitleReference } from '../../shared/api/types'
import { selectedSubtitleAvailability, subtitleDocumentMessage, subtitlePartMessage } from './subtitleAvailability'
import { subtitlePriority, useSubtitles } from './useSubtitles'
import { isChineseTrack } from '../../shared/text/chineseText'
import { activeSubtitleCue } from './activeCue'
import { TranscriptionControls } from './TranscriptionControls'
import { transcriptParagraphs, transcriptSeparator, transcriptText } from './transcriptParagraphs'
import './subtitles.css'
import { useReaderPlayback } from '../../shared/reader/ReaderActions'
import { subtitleTrackHash } from '../../shared/media/subtitleSource'
import { readExcerptRadius, rememberExcerptRadius, subtitleExcerpt, type ExcerptRadius } from './subtitleExcerpt'
import { useSubtitleExport } from './useSubtitleExport'
import { highlightMatch, timestamp } from './subtitlePresentation'

export function SubtitleReader({ item, source = 'bilibili', position = null, onSeek, onTrackChange, onAvailabilityChange, assistance, onNoteAt }: {
  item: Pick<ContentItem, 'platformId' | 'url'> & Partial<Pick<ContentItem,'kind' | 'title' | 'author'>>; source?: 'bilibili' | 'youtube' | 'podcast' | 'local'; onNoteAt?: (position: number, reference?: SubtitleReference) => void
  assistance?: SubtitleAssistance
  position?: number | null; onSeek?: (position: number) => void; onTrackChange?: (track: SubtitleTrack | null) => void; onAvailabilityChange?: (status: SubtitleAvailability | 'available' | 'login_required' | null) => void
}) {
  const subtitles = useSubtitles(item.platformId, source)
  const { playing } = useReaderPlayback()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const settingsId = useId()
  const [language, setLanguage] = useState('')
  const [query, setQuery] = useState('')
  const [visibleCount, setVisibleCount] = useState(200)
  const [follow, setFollow] = useState(true)
  const [followPaused, setFollowPaused] = useState(false)
  const [viewportRevision, setViewportRevision] = useState(0)
  useEffect(() => {
    const resize = () => setViewportRevision(value => value + 1)
    window.addEventListener('resize', resize)
    return () => window.removeEventListener('resize', resize)
  }, [])
  const [confirmSave, setConfirmSave] = useState<SubtitleTrack | null>(null)
  const [deleteLanguage, setDeleteLanguage] = useState<string | null>(null)
  const [mode, setMode] = useState<'read' | 'article' | 'learn'>('read')
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null)
  const [loop, setLoop] = useState(false)
  const [hideText, setHideText] = useState(false)
  const [excerptRadius, setExcerptRadius] = useState<ExcerptRadius>(readExcerptRadius)
  const [excerptError, setExcerptError] = useState('')
  const loopRequested = useRef(false)
  const list = useRef<HTMLOListElement>(null)
  const doc = subtitles.document
  const track = doc?.tracks.find(candidate => candidate.language === language) ?? doc?.tracks.reduce<SubtitleTrack | undefined>((best, candidate) => !best || subtitlePriority(candidate) < subtitlePriority(best) ? candidate : best, undefined)
  const trackHash = useMemo(() => track ? subtitleTrackHash(track) : '', [track])
  const alreadyChinese = useMemo(() => isChineseTrack(track), [track])
  const translated = !alreadyChinese && assistance?.track === track ? assistance?.texts : undefined
  const hasTranslations = Boolean(track?.cues.some((_, index) => translated?.[index]?.trim()))
  const translationsComplete = Boolean(track?.cues.length && track.cues.every((_, index) => translated?.[index]?.trim()))
  const {
    exportMode,
    exportNotice,
    exportError,
    selectExportMode,
    exportSRT,
    exportDocument,
  } = useSubtitleExport({ item, source, track, translated, hasTranslations, translationsComplete })
  const cueIndices = useMemo(() => new Map(track?.cues.map((cue, index) => [cue, index]) ?? []), [track])
  const choices = subtitles.origin === 'platform' && source !== 'bilibili' && doc?.languages?.length ? doc.languages : doc?.tracks ?? []
  const atOriginal = (seconds: number) => source === 'podcast' || source === 'local' ? item.url : source === 'youtube' ? `https://www.youtube.com/watch?v=${encodeURIComponent(item.platformId)}&t=${Math.floor(seconds)}s` : `${item.url.split('?')[0]}?p=1&t=${Math.floor(seconds)}`
  const filter = query.trim().toLocaleLowerCase()
  const cues = useMemo(() => track?.cues.filter(cue => !filter || transcriptText(cue.text).toLocaleLowerCase().includes(filter) || transcriptText(translated?.[cueIndices.get(cue) ?? -1] ?? '').toLocaleLowerCase().includes(filter)) ?? [], [track, filter, translated, cueIndices])
  const paragraphs = useMemo(() => transcriptParagraphs(cues.slice(0, visibleCount)), [cues, visibleCount])
  const activeCue = track ? activeSubtitleCue(track.cues, position) : undefined

  useEffect(() => {
    setLanguage(''); setQuery(''); setVisibleCount(200); setSettingsOpen(false)
  }, [item.platformId, source])
  useEffect(() => { setExcerptError('') }, [item.platformId, source, trackHash])
  function excerpt(selected: readonly SubtitleCue[], radius = excerptRadius) {
    if (!track || !onNoteAt) return
    setExcerptError('')
    try {
      const reference = subtitleExcerpt(track, selected, radius, trackHash)
      onNoteAt(selected[0].start, reference)
    } catch (cause) {
      setExcerptError(cause instanceof Error ? cause.message : '摘录未能加入，请重新选择。')
    }
  }

  useEffect(() => { onTrackChange?.(track ?? null) }, [track, onTrackChange])
  const availability = selectedSubtitleAvailability(doc, track, subtitles.origin)
  useEffect(() => { onAvailabilityChange?.(availability) }, [availability, onAvailabilityChange])
  useEffect(() => () => onTrackChange?.(null), [onTrackChange])
  useEffect(() => {
    if (!follow || followPaused || filter || !activeCue || !onSeek) return
    const index = cues.indexOf(activeCue)
    if (index >= visibleCount) {
      setVisibleCount(Math.ceil((index + 1) / 200) * 200)
      return
    }
    const container = list.current
    const row = container?.querySelector<HTMLElement>('[data-subtitle-active="true"], [aria-current="true"]')
    if (container && row) {
      const viewport = container.getBoundingClientRect()
      const bounds = row.getBoundingClientRect()
      const viewportTop = viewport.top + container.clientTop
      const top = container.scrollTop + bounds.top - viewportTop
      if (Math.abs(Math.max(0, Math.min(top, container.scrollHeight - container.clientHeight)) - container.scrollTop) < 1) return
      container.scrollTo({ top, behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    }
  }, [follow, followPaused, filter, activeCue, onSeek, cues, visibleCount, mode, viewportRevision, settingsOpen])

  function pauseFollow() {
    if (!follow || !onSeek || position === null) return
    setFollowPaused(true)
    // Stop an in-flight automatic scroll before the user's own scroll runs.
    list.current?.scrollTo({ top: list.current.scrollTop, behavior: 'auto' })
  }

  useEffect(() => {
    const pane = list.current
    if (!pane) return
    const loadNextCues = () => {
      if (!pane.clientHeight || cues.length <= visibleCount ||
          pane.scrollTop + pane.clientHeight < pane.scrollHeight - 160) return
      setVisibleCount(Math.min(cues.length, visibleCount + 200))
    }
    pane.addEventListener('scroll', loadNextCues, { passive: true })
    return () => pane.removeEventListener('scroll', loadNextCues)
  }, [cues.length, visibleCount, mode])

  const studyCue = track?.cues[selectedIndex ?? (activeCue ? Math.max(0, track.cues.indexOf(activeCue)) : 0)]
  useEffect(() => { setSelectedIndex(null); setLoop(false); loopRequested.current = false }, [track?.language])
  useEffect(() => {
    if (!loop || !studyCue || !onSeek || position === null) return
    if (position < studyCue.end) { loopRequested.current = false; return }
    if (!loopRequested.current) { loopRequested.current = true; onSeek(studyCue.start) }
  }, [loop, studyCue, position, onSeek])
  function chooseCue(index: number) {
    if (!track) return
    const next = Math.max(0, Math.min(track.cues.length - 1, index))
    setSelectedIndex(next); loopRequested.current = false; onSeek?.(track.cues[next].start)
  }
  const transcription = <TranscriptionControls video={item.kind==='video'} contentId={source === 'podcast' || source === 'local' ? item.platformId : `${source}:${item.platformId}`} onComplete={() => { void subtitles.reloadSaved() }}/>
  const revealTranscription = () => {
    setSettingsOpen(true)
    const management = document.querySelector<HTMLDetailsElement>('[data-subtitle-management]')
    if (management) management.open = true
    window.setTimeout(() => {
      const control = document.querySelector<HTMLElement>('[data-transcription-control]')
      control?.scrollIntoView({ block: 'center', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    }, 0)
  }

  return <section className={`subtitle-reader${track ? ' subtitle-reader-loaded' : ''}`} data-playing={playing ? 'true' : undefined} data-settings-open={settingsOpen} aria-label="字幕与阅读">
    <div className="subtitle-settings-bar">
      <h2 id="subtitle-title"><Captions size={16} aria-hidden="true"/>字幕</h2>
      <button type="button" className="subtitle-settings-toggle" data-subtitle-settings-toggle aria-expanded={settingsOpen} aria-controls={settingsId} onClick={() => setSettingsOpen(value => !value)}><SlidersHorizontal size={14} aria-hidden="true"/>{settingsOpen ? '收起设置' : '字幕设置'}{settingsOpen ? <ChevronUp size={14} aria-hidden="true"/> : <ChevronDown size={14} aria-hidden="true"/>}</button>
    </div>
    <div id={settingsId} className="subtitle-settings-panel" hidden={!settingsOpen}>
    <div className="subtitle-heading">
      {track && assistance?.track === track && <div className="subtitle-assistance">{assistance.controls}</div>}
      {track && <div className="subtitle-modes" aria-label="字幕阅读模式"><button aria-pressed={mode === 'read'} onClick={() => { setMode('read'); setLoop(false) }}>阅读</button><button aria-pressed={mode === 'article'} onClick={() => { setMode('article'); setLoop(false) }}>文章</button><button aria-pressed={mode === 'learn'} onClick={() => { setMode('learn'); setLoop(false); setSelectedIndex(null) }}>语言学习</button></div>}
      <details className="subtitle-management" data-subtitle-management><summary>字幕选项</summary><div className="subtitle-management-menu">
        {subtitles.loading ? <button className="button button-secondary" onClick={subtitles.cancel}><X size={15} />取消获取</button> : <button className="button button-secondary" disabled={subtitles.pending} onClick={() => { setConfirmSave(null); setDeleteLanguage(null); setVisibleCount(200); void subtitles.read() }}>{doc || subtitles.error ? '重新获取字幕' : '读取字幕'}</button>}
        <div className="saved-subtitle-tools"><span><HardDrive size={14}/>{subtitles.localLoading ? '正在读取本机字幕' : !subtitles.localKnown ? '本机字幕状态未知' : subtitles.origin === 'saved' && subtitles.saved.length ? '正在阅读本机字幕' : `已保存 ${subtitles.saved.length} 种语言`}</span>{subtitles.origin !== 'saved' && subtitles.saved.length > 0 && <button className="button button-secondary" disabled={subtitles.pending} onClick={() => { setVisibleCount(200); subtitles.useSaved() }}>使用本机字幕</button>}<button className="button button-secondary" disabled={subtitles.localLoading || subtitles.pending} onClick={() => { setConfirmSave(null); setDeleteLanguage(null); void subtitles.reloadSaved() }}>重新读取本机字幕</button></div>
        {track && <div className="saved-subtitle-actions"><button className="button button-secondary" onClick={() => exportSRT()}><ArrowDownToLine size={15}/>导出 SRT</button>{subtitles.origin === 'platform' ? <button className="button button-secondary" disabled={!subtitles.localKnown || subtitles.loading || subtitles.pending} onClick={() => { setDeleteLanguage(null); const existing = subtitles.saved.some(value => value.track.language === track.language); if (existing) setConfirmSave(track); else void subtitles.save(track) }}><Save size={15}/>{subtitles.pending ? '正在保存' : '保存此语言到本机'}</button> : <button className="button button-secondary" disabled={!subtitles.localKnown || subtitles.pending || subtitles.localLoading} onClick={() => { setConfirmSave(null); setDeleteLanguage(track.language) }}>移除此语言的本机字幕</button>}</div>}
        {track && hasTranslations && <div className="saved-subtitle-actions"><button className="button button-secondary" title="未翻译段落保留原文" onClick={() => exportSRT('bilingual')}><ArrowDownToLine size={15}/>导出双语 SRT</button><button className="button button-secondary" disabled={!translationsComplete} title={translationsComplete ? '导出完整中文译文' : '全部段落翻译完成后可导出'} onClick={() => exportSRT('translated')}><ArrowDownToLine size={15}/>导出译文 SRT</button></div>}
        {track && <div className="subtitle-document-export">
          <div className="subtitle-export-modes" role="group" aria-label="文稿内容">
            {([['original', '原文'], ['bilingual', '双语'], ['translated', '译文']] as const).map(([value, label]) => <button key={value} type="button" aria-pressed={exportMode === value} disabled={value === 'bilingual' ? !hasTranslations : value === 'translated' ? !translationsComplete : false} title={value === 'translated' && !translationsComplete ? '全部翻译完成后可选' : value === 'bilingual' ? '未翻译段落保留原文' : undefined} onClick={() => selectExportMode(value)}>{label}</button>)}
          </div>
          <button className="button button-secondary" title="保存为 Markdown 文稿" onClick={exportDocument}><ArrowDownToLine size={15} aria-hidden="true"/>导出文稿</button>
          {exportNotice && <p role="status">{exportNotice}</p>}
          {exportError && <p role="alert">{exportError}</p>}
        </div>}
        {confirmSave && <div className="saved-subtitle-confirm"><p>本机已有 {confirmSave.label}，要用当前平台版本覆盖吗？</p><button className="button button-secondary" disabled={subtitles.pending} onClick={() => setConfirmSave(null)}>保留本机版本</button><button className="button button-primary" disabled={!subtitles.localKnown || subtitles.pending || subtitles.localLoading} onClick={() => { void subtitles.save(confirmSave); setConfirmSave(null) }}>确认覆盖字幕</button></div>}
        {deleteLanguage && <div className="saved-subtitle-confirm"><p>移除 {deleteLanguage} 的本机字幕？笔记和视频文件保留。</p><button className="button button-secondary" disabled={subtitles.pending} onClick={() => setDeleteLanguage(null)}>保留本机字幕</button><button className="button button-primary" disabled={!subtitles.localKnown || subtitles.pending || subtitles.localLoading} onClick={() => { void subtitles.remove(deleteLanguage).then(ok => { if (ok) setDeleteLanguage(null) }) }}>确认移除字幕</button></div>}
        {source === 'youtube' && <p className="subtitle-note">中文优先，其次英文。先读取已有字幕；英文字幕可在下方按需翻译为简体中文。</p>}
        {track && transcription}
      </div></details>
    </div>
    {track && <div className="subtitle-toolbar">
      <label className="subtitle-language"><span className="visually-hidden">字幕语言</span><select aria-label="字幕语言" value={track.language} disabled={subtitles.loading || subtitles.pending} onChange={event => { const selected = event.target.value; setLanguage(selected); setVisibleCount(200); if (source === 'youtube' && subtitles.origin === 'platform' && !doc?.tracks.some(value => value.language === selected)) void subtitles.read(selected, true) }}>{choices.map(candidate => <option key={candidate.language} value={candidate.language}>{candidate.label}{candidate.automatic ? ' · 自动' : ''}</option>)}</select></label>
      <label className="subtitle-search"><Search size={15} aria-hidden="true"/><input type="search" aria-label="搜索字幕" placeholder="搜索字幕" value={query} onChange={event => { setQuery(event.target.value); setVisibleCount(200) }} maxLength={200}/>{query && <button type="button" className="subtitle-search-clear" aria-label="清除字幕搜索" title="清除搜索" onClick={() => { setQuery(''); setVisibleCount(200) }}><X size={14} aria-hidden="true"/></button>}</label>
      <label className="subtitle-follow"><input type="checkbox" checked={follow} disabled={!onSeek} onChange={event => { setFollow(event.target.checked); setFollowPaused(false) }}/>跟随播放</label>
    </div>}
    {track && onNoteAt && <div className="subtitle-excerpt-range">
      <span>摘录范围</span><div role="group" aria-label="摘录范围">
        {([0, 1, 2] as const).map(value => <button type="button" key={value} aria-pressed={excerptRadius === value} onClick={() => { setExcerptRadius(value); rememberExcerptRadius(value); setExcerptError('') }}>{value === 0 ? mode === 'article' ? '本段' : '本句' : value === 1 ? '前后一句' : '前后两句'}</button>)}
      </div>
    </div>}
    </div>
    {!doc && !subtitles.loading && !subtitles.error && <p className="subtitle-note">{source==='local'?`读取已保存的本机字幕。当前${item.platformId.startsWith('xiaohongshu:')?'小红书':'抖音'}适配不读取平台原字幕，可主动转录。`:'读取平台已有字幕，视频和音频无需保存到本机。'}</p>}
    {subtitles.loading && <p className="subtitle-note" role="status"><LoaderCircle size={16} className="spin" />{subtitles.localLoading ? '正在读取本机字幕…' : '正在获取已有字幕…'}</p>}
    {subtitles.localError && <p className="subtitle-error" role="alert">{subtitles.localError} {source==='local'?'请重试读取本机字幕。':'本机保存暂不可用，平台字幕仍可读取。'}</p>}
    {subtitles.writeError && <p className="subtitle-error" role="alert">{subtitles.writeError} 已确认记录仍保留，请先重新读取本机字幕。</p>}
    {subtitles.notice && <p className="subtitle-note" role="status">{subtitles.notice}</p>}
    {subtitles.error && <p className="subtitle-error" role="alert">{subtitles.error}{doc && ' 下方保留上一次获取的结果。'}</p>}
    {!doc?.parts?.length && doc && ['rate_limited', 'api_empty', 'error'].includes(doc.status) && <div className={doc.status === 'error' ? 'subtitle-error' : 'subtitle-empty'} role={doc.status === 'error' ? 'alert' : 'status'}><p>{subtitleDocumentMessage(doc.status, doc.errorCode, doc.errorMessage)}</p>{doc.status !== 'api_empty' && <button type="button" className="text-button" onClick={() => void subtitles.read()}>{doc.status === 'rate_limited' ? '稍后重试' : '重试'}</button>}{doc.status === 'api_empty' && <a href="/tools#accounts">去账号设置</a>}</div>}
    {excerptError && <div className="subtitle-excerpt-error" role="alert"><span>{excerptError}</span><button className="text-button" onClick={() => { setMode('read'); setLoop(false); setExcerptRadius(0); rememberExcerptRadius(0); setExcerptError('') }}>改用本句</button></div>}
    {doc?.parts && <div className="subtitle-parts" aria-label="各分 P 字幕状态">{doc.parts.map(part => { const presentation = subtitlePartMessage(part); const current = part.page === doc.part; const retry = presentation.action === '重试' || presentation.action === '稍后重试'; const candidates = part.languages.map(candidate => `${candidate.label} · ${candidate.source === 'cc' ? 'CC' : candidate.source === 'yt-dlp' ? 'yt-dlp' : 'AI'}${candidate.hasUrl ? '' : '（字幕地址为空）'}`).join('、'); return <div key={`${part.page}-${part.cid}`} className={`subtitle-part-status subtitle-part-${presentation.tone}`}><span>第 {part.page} 分 P{part.title ? ` · ${part.title}` : ''}</span><span>{presentation.message}</span>{candidates && <span className="subtitle-part-candidates">字幕候选：{candidates}</span>}{current && retry && <button type="button" className="text-button" onClick={() => void subtitles.read()}>{presentation.action}</button>}{retry && !current && <span className="subtitle-part-retry-note">如需重试，请重新获取全部分 P 字幕。</span>}{current && presentation.action === '去账号设置' && <a className="text-button" href="/tools#accounts">去账号设置</a>}{current && (presentation.action === '改用转录' || presentation.action === '转录') && <button type="button" className="text-button" onClick={revealTranscription}>{presentation.action === '转录' ? '转录' : '改用转录'}</button>}{presentation.action === '去账号设置' && !current && <a className="text-button" href="/tools#accounts">去账号设置</a>}</div> })}</div>}
    {track?.mismatch && <div className="subtitle-empty subtitle-mismatch-note" role="status"><p>这条 AI 字幕疑似与视频标题不匹配，但字幕仍可查看和用于摘要。</p><button type="button" className="text-button" onClick={revealTranscription}>改用转录</button></div>}
    {!doc?.parts?.length && doc?.status === 'none' && <p className="subtitle-note" role="status">{source === 'local' ? `没有已保存的本机字幕。当前${item.platformId.startsWith('xiaohongshu:')?'小红书':'抖音'}适配不读取平台原字幕，可主动转录。` : source === 'podcast' ? '还没有逐字字幕，可以按需转录。' : source === 'youtube' ? '这个视频暂无可用的原始字幕，可以按需转录。' : '第一分 P 暂无可用字幕，可以按需转录。'}</p>}
    {doc?.status === 'login_required' && <div className="subtitle-empty" role="status"><p>平台提示读取字幕需要登录。</p><p>在工具设置保存你的 {source === 'youtube' ? 'YouTube' : 'B站'} Cookie，再重新获取字幕。</p></div>}
    {!track && transcription}
    {track && <>
      <div className="subtitle-summary"><span role="status">{filter ? `匹配 ${cues.length} / ${track.cues.length} 段` : `${track.cues.length} 段字幕`}{subtitles.origin === 'saved' && ' · 本机副本'}{track.source === 'cc' ? ' · CC' : track.source === 'ai' ? ' · AI' : track.source === 'yt-dlp' ? ' · yt-dlp' : ''}{track.mismatch && ' · 疑似错位'}{track.automatic && ' · 自动字幕'}</span>{filter ? <span>· 搜索时暂停跟随</span> : follow && followPaused ? <button className="subtitle-resume" onClick={() => setFollowPaused(false)}>继续跟随播放</button> : null}</div>
      {mode === 'learn' && studyCue && <div className="subtitle-study" aria-label="逐句学习">
        <div className="subtitle-study-heading"><span>逐句精听 · {timestamp(studyCue.start)}</span><button aria-pressed={hideText} onClick={() => setHideText(value => !value)}>{hideText ? '显示原文' : '隐藏原文'}</button></div>
        <p>{hideText ? '先听一遍，再显示原文。' : transcriptText(studyCue.text)}</p>
        {!hideText && translated?.[cueIndices.get(studyCue) ?? -1] && <p className="subtitle-translated" lang="zh-Hans">{translated[cueIndices.get(studyCue)!]}</p>}
        <div className="subtitle-study-actions"><button className="icon-button" aria-label="上一句" disabled={!onSeek || track.cues.indexOf(studyCue) === 0} onClick={() => chooseCue(track.cues.indexOf(studyCue) - 1)}><ChevronLeft size={17}/></button><button className="button button-secondary" disabled={!onSeek} onClick={() => onSeek?.(studyCue.start)}><Repeat2 size={14}/>回到句首</button><button className="icon-button" aria-label="下一句" disabled={!onSeek || track.cues.indexOf(studyCue) === track.cues.length - 1} onClick={() => chooseCue(track.cues.indexOf(studyCue) + 1)}><ChevronRight size={17}/></button><label><input type="checkbox" checked={loop} disabled={!onSeek} onChange={event => { setLoop(event.target.checked); setSelectedIndex(track.cues.indexOf(studyCue)); if(event.target.checked)onSeek?.(studyCue.start) }}/>循环本句</label></div>
      </div>}
      {cues.length > 0 ? <ol ref={list} className={`subtitle-cues ${onSeek ? 'synchronized' : ''} ${mode === 'article' ? 'subtitle-article-mode' : ''}`} aria-label="字幕段落" tabIndex={0} onWheel={pauseFollow} onTouchMove={pauseFollow} onKeyDown={event => { if (event.target === event.currentTarget && ['ArrowUp','ArrowDown','PageUp','PageDown','Home','End',' '].includes(event.key)) pauseFollow() }} onPointerDown={event => { const bounds = event.currentTarget.getBoundingClientRect(); if (event.target === event.currentTarget && event.clientX >= bounds.left + event.currentTarget.clientWidth) pauseFollow() }}>{mode === 'article' ? paragraphs.map((paragraph, index) => <li key={`${paragraph[0].start}-${index}`}>
        <div className="subtitle-cue-text">
          <p>{paragraph.map((cue, cueIndex) => <span key={cueIndices.get(cue)} data-subtitle-active={activeCue === cue ? 'true' : undefined} data-cue-start={cue.start}>{cueIndex > 0 && transcriptSeparator(transcriptText(paragraph[cueIndex - 1].text), transcriptText(cue.text))}{highlightMatch(transcriptText(cue.text), filter)}</span>)}</p>
          {paragraph.some(cue => translated?.[cueIndices.get(cue) ?? -1]) && <p className="subtitle-translated" lang="zh-Hans">{paragraph.map(cue => ({ cue, text: transcriptText(translated?.[cueIndices.get(cue) ?? -1] ?? '') })).filter(value => value.text).map(({cue, text}, cueIndex, values) => <span key={cueIndices.get(cue)} data-subtitle-active={activeCue === cue ? 'true' : undefined}>{cueIndex > 0 && transcriptSeparator(values[cueIndex - 1].text, text)}{highlightMatch(text, filter)}</span>)}</p>}
          {onNoteAt && <div className="subtitle-excerpt-actions"><button type="button" className="subtitle-note-button" aria-label={`记录字幕时间 ${timestamp(paragraph[0].start)}`} title="关联这段开始的时间，写下想法" onClick={() => onNoteAt(paragraph[0].start, undefined)}><StickyNote size={13} aria-hidden="true"/>记笔记</button><button type="button" className="subtitle-excerpt-button" aria-label={`摘录字幕原句 ${timestamp(paragraph[0].start)}`} title="保存这段原文与时间" onClick={() => excerpt(paragraph)}><Quote size={13} aria-hidden="true"/>摘录</button></div>}
        </div>
      </li>) : cues.slice(0, visibleCount).map((cue, index) => <li key={`${cue.start}-${index}`} aria-current={activeCue === cue ? 'true' : undefined}>
        <div className="subtitle-cue-actions">{onSeek ? <button className="subtitle-time" title={`跳转到 ${timestamp(cue.start)}`} aria-label={`跳转播放时间 ${timestamp(cue.start)}`} onClick={() => { if(mode==='learn')setSelectedIndex(track.cues.indexOf(cue)); onSeek(cue.start) }}><time>{timestamp(cue.start)}</time></button> : <a className="subtitle-time" href={atOriginal(cue.start)} target="_blank" rel="noopener noreferrer" aria-label={source === 'podcast' ? '打开原节目' : `在原视频打开 ${timestamp(cue.start)}`}><time>{timestamp(cue.start)}</time><ExternalLink size={12}/></a>}</div>
        <div className="subtitle-cue-text"><p>{highlightMatch(transcriptText(cue.text), filter)}</p>{translated?.[cueIndices.get(cue) ?? -1] && <p className="subtitle-translated" lang="zh-Hans">{highlightMatch(transcriptText(translated[cueIndices.get(cue)!]), filter)}</p>}</div>
        {onNoteAt && <div className="subtitle-row-tools"><button type="button" className="subtitle-note-button" aria-label={`记录字幕时间 ${timestamp(cue.start)}`} title="关联这个时间，写下想法" onClick={() => onNoteAt(cue.start)}><StickyNote size={13} aria-hidden="true"/>记笔记</button><button type="button" className="subtitle-excerpt-button" aria-label={`摘录字幕原句 ${timestamp(cue.start)}`} title="保存这句原文与时间" onClick={() => excerpt([cue])}><Quote size={13} aria-hidden="true"/>摘录</button></div>}
      </li>)}</ol> : <p className="subtitle-note">没有匹配的字幕，试试其他关键词。</p>}
      {cues.length > visibleCount && <button className="button button-secondary subtitle-more" onClick={() => setVisibleCount(count => Math.min(cues.length, count + 200))}>继续阅读字幕（已显示 {visibleCount} / {cues.length} 段）</button>}
    </>}
  </section>
}
