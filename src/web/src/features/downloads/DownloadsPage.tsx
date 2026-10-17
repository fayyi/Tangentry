import { ArrowDownToLine, Check, ChevronDown, CircleHelp, ExternalLink, FileAudio, Folder, Link2, LoaderCircle, Play, RefreshCw, Settings2, Subtitles, Trash2, Video, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { useDownloads } from './useDownloads'
import type { DownloadFormat, DownloadJob, DownloadOptions } from '../../shared/api/types'
import { formatDate } from '../../shared/ui/ContentCard'
import { PageHeader } from '../../shared/ui/PageHeader'
import './downloads.css'

const labels = { queued: '等待中', running: '下载与处理中', completed: '已完成', failed: '下载失败', cancelled: '已取消', interrupted: '已中断' }
type DownloadState = ReturnType<typeof useDownloads>
const defaults: DownloadOptions = { mode: 'video', container: 'mp4', videoOnly: false, audioFormat: 'best', compress: true, subtitleLanguages: [], automaticSubtitles: false }
const hasVideo = (format: DownloadFormat) => !!format.videoCodec && format.videoCodec !== 'none'
const hasAudio = (format: DownloadFormat) => !!format.audioCodec && format.audioCodec !== 'none'
function bytes(size = 0) {
  if (!size) return ''
  return size >= 1024 ** 3 ? `${(size / 1024 ** 3).toFixed(2)} GB` : `${(size / 1024 ** 2).toFixed(1)} MB`
}
function duration(seconds: number) {
  return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`
}
function canRead(job: DownloadJob) {
  return job.mode !== 'audio' && /^(https:\/\/www\.bilibili\.com\/video\/BV|https:\/\/www\.youtube\.com\/watch\?|https:\/\/www\.douyin\.com\/video\/|https:\/\/www\.xiaohongshu\.com\/explore\/)/.test(job.url)
}
function jobOptions(job: DownloadJob): DownloadOptions {
  return { mode: job.mode, container: job.container, videoOnly: job.videoOnly, audioFormat: job.audioFormat, videoFormatId: job.videoFormatId, audioFormatId: job.audioFormatId, subtitleLanguages: job.subtitleLanguages, automaticSubtitles: job.automaticSubtitles, compress: job.compress }
}

function FormatChoices({ formats, selected, onChange, label }: { formats: DownloadFormat[]; selected: string; onChange: (id: string) => void; label: string }) {
  return <fieldset className="download-format-group">
    <legend>{label} <span className="count">{formats.length}</span></legend>
    <div className="download-format-list">
      <label className="download-format-row"><input type="radio" name={label} aria-label={`${label}自动选择`} checked={!selected} onChange={() => onChange('')} /><span><strong>自动选择最佳{label === '视频格式' ? '画质' : '音轨'}</strong></span><span className="format-detail">推荐</span></label>
      {formats.map(format => <label className="download-format-row" key={format.id}>
        <input type="radio" name={label} aria-label={`${label} ${format.id}`} checked={selected === format.id} onChange={() => onChange(format.id)} />
        <span className="format-name"><strong>{hasVideo(format) ? format.height ? `${format.height}p` : '原始分辨率' : format.audioBitrate ? `${Math.round(format.audioBitrate)} kbps` : '原始音轨'} <small>{format.ext.toUpperCase()}</small></strong><span>{[hasVideo(format) ? format.videoCodec : format.audioCodec, format.fps ? `${format.fps} fps` : '', hasVideo(format) && hasAudio(format) ? '含音频' : '', `编号 ${format.id}`].filter(Boolean).join(' · ')}</span></span>
        <span className="format-detail">{format.fileSize ? `${format.approximate ? '约 ' : ''}${bytes(format.fileSize)}` : '大小未知'}</span>
      </label>)}
    </div>
  </fieldset>
}

export function DownloadsPage({ downloads, onRead, intent }: { downloads: DownloadState; onRead: (job: DownloadJob) => void; intent?: { url: string; mode: 'video' | 'audio'; token: number } | null }) {
  const [url, setURL] = useState(intent?.url ?? '')
  const [height, setHeight] = useState(0)
  const [options, setOptions] = useState<DownloadOptions>(() => ({ ...defaults, mode: intent?.mode ?? 'video' }))
  const [filter, setFilter] = useState<'all' | 'active' | 'completed' | 'failed'>('all')
  const [settingsOpen, setSettingsOpen] = useState(() => matchMedia('(min-width: 761px)').matches)
  const [thumbnailFailed, setThumbnailFailed] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleteDialog, setDeleteDialog] = useState<{ ids: string[]; clear: boolean } | null>(null)
  const info = downloads.info
  const videoFormats = info?.formats.filter(hasVideo) ?? []
  const audioFormats = info?.formats.filter(format => hasAudio(format) && !hasVideo(format)) ?? []
  const audioAvailable = info?.formats.some(hasAudio) ?? false
  const heights = [...new Set(videoFormats.map(format => format.height).filter(value => value > 0))].sort((a, b) => b - a)
  const audioOnly = options.mode === 'audio'
  const activeCount = downloads.jobs.filter(job => job.state === 'running' || job.state === 'queued').length
  const taskFilters = [
    { value: 'all' as const, label: '全部', count: downloads.jobs.length },
    { value: 'active' as const, label: '进行中', count: activeCount },
    { value: 'completed' as const, label: '已完成', count: downloads.jobs.filter(job => job.state === 'completed').length },
    { value: 'failed' as const, label: '需重试', count: downloads.jobs.filter(job => job.state === 'failed' || job.state === 'interrupted').length },
  ]
  const jobs = downloads.jobs.filter(job => filter === 'all' || (filter === 'active' ? job.state === 'running' || job.state === 'queued' : filter === 'failed' ? job.state === 'failed' || job.state === 'interrupted' : job.state === 'completed'))
  const deletable = jobs.filter(job => job.state !== 'queued' && job.state !== 'running')
  const allSelected = deletable.length > 0 && deletable.every(job => selected.has(job.id))
  const ready = !!info && (audioOnly ? audioAvailable : videoFormats.length > 0) && downloads.tools?.downloadReady
  const subtitleTracks = (info?.subtitles ?? []).filter(track => !track.automatic || options.automaticSubtitles)
  const languages = [...new Map(subtitleTracks.map(track => [track.language, track])).values()]
  function update(value: Partial<DownloadOptions>) { setOptions(current => ({ ...current, ...value })) }
  useEffect(() => {
    const media = matchMedia('(min-width: 761px)')
    const resize = () => setSettingsOpen(media.matches)
    media.addEventListener('change', resize)
    return () => media.removeEventListener('change', resize)
  }, [])
  useEffect(() => {
    if (intent?.url) {
      setURL(intent.url)
      setOptions({ ...defaults, mode: intent.mode })
      setHeight(0)
      downloads.clearInfo()
    }
  }, [intent?.token])
  useEffect(() => {
    setThumbnailFailed(false)
    if (!info) return
    setOptions(current => ({ ...current, mode: info.formats.some(hasVideo) ? current.mode : 'audio', videoOnly: info.formats.some(hasAudio) ? current.videoOnly : true, videoFormatId: '', audioFormatId: '', subtitleLanguages: [] }))
    setHeight(0)
  }, [info])

  async function start() {
    if (!info || !ready) return
    const ok = await downloads.start(info.url, info.title, height, { ...options, compress: !audioOnly && options.compress, videoOnly: !audioOnly && options.videoOnly })
    if (ok) { setURL(''); downloads.clearInfo() }
  }

  async function confirmDelete(deleteFiles: boolean) {
    if (!deleteDialog) return
    let success = true
    if (deleteDialog.clear) success = await downloads.clear(deleteFiles)
    else for (const id of deleteDialog.ids) success = (await downloads.remove(id, deleteFiles)) && success
    if (success) {
      setSelected(new Set())
      setDeleteDialog(null)
    }
  }

  const settingsPanel = <aside className="download-settings" aria-label="下载设置">
    <details className="download-options-disclosure" open={settingsOpen} onToggle={event => setSettingsOpen(event.currentTarget.open)}>
      <summary><Settings2 size={16} aria-hidden="true"/><strong>下载设置</strong><span>{audioOnly ? options.audioFormat === 'best' ? '原始音频' : options.audioFormat?.toUpperCase() : `${options.container?.toUpperCase()} · ${height ? `${height}p` : '最高画质'}`}</span><ChevronDown size={16} aria-hidden="true"/></summary>
      <div className="download-settings-body">
        <div className="download-setting-block">
          <span className="download-setting-label" id="download-kind-label">下载类型</span>
          <div className="download-segment" role="group" aria-labelledby="download-kind-label">
            <button aria-pressed={!audioOnly} disabled={!!info && !videoFormats.length} onClick={() => update({ mode: 'video' })}><Video size={15} />视频</button>
            <button aria-pressed={audioOnly} disabled={!!info && !audioAvailable} onClick={() => update({ mode: 'audio' })}><FileAudio size={15} />音频</button>
          </div>
        </div>
        {!audioOnly ? <>
          <div className="download-setting-block">
            <span className="download-setting-label" id="download-container-label">文件格式</span>
            <div className="download-segment" role="group" aria-labelledby="download-container-label">{(['mp4', 'mkv'] as const).map(container => <button key={container} aria-pressed={options.container === container} onClick={() => update({ container })}>{container.toUpperCase()}</button>)}</div>
          </div>
          <label className="download-setting-block"><span className="download-setting-label">下载画质</span><select aria-label="下载画质" value={height} disabled={!info || !!options.videoFormatId} onChange={event => setHeight(Number(event.target.value))}><option value={0}>最高原画</option>{heights.map(value => <option key={value} value={value}>最高 {value}p</option>)}</select></label>
          <div className="download-checks">
            <label className="download-check"><input type="checkbox" checked={!options.videoOnly} disabled={!!info && !audioAvailable} onChange={event => update({ videoOnly: !event.target.checked })} /><span>保留音频</span></label>
            <label className="download-check"><input type="checkbox" checked={!!options.compress} onChange={event => update({ compress: event.target.checked })} /><span>节省空间</span></label>
          </div>
        </> : <label className="download-setting-block"><span className="download-setting-label">音频格式</span><select aria-label="音频格式" value={options.audioFormat} onChange={event => update({ audioFormat: event.target.value as DownloadOptions['audioFormat'] })}><option value="best">原始音质</option><option value="mp3">MP3</option><option value="m4a">M4A</option><option value="flac">FLAC</option><option value="wav">WAV</option><option value="opus">Opus</option></select></label>}
        <section className="download-setting-section" aria-label="字幕设置">
          <h3><Subtitles size={15} aria-hidden="true" />字幕</h3>
          <label className="download-check"><input type="checkbox" checked={!!options.automaticSubtitles} onChange={event => {
            const automaticSubtitles = event.target.checked
            const manual = new Set(info?.subtitles.filter(track => !track.automatic).map(track => track.language))
            update({ automaticSubtitles, subtitleLanguages: automaticSubtitles ? options.subtitleLanguages : options.subtitleLanguages?.filter(language => manual.has(language)) })
          }} /><span>包含自动字幕</span></label>
          {!info ? <p className="download-muted">获取信息后显示可用字幕</p> : languages.length === 0 ? <p className="download-muted">没有可用{options.automaticSubtitles ? '' : '人工'}字幕</p> : <div className="download-languages">{languages.map(track => <label className="download-check" key={track.language}><input type="checkbox" aria-label={`下载字幕 ${track.language}`} checked={options.subtitleLanguages?.includes(track.language) ?? false} onChange={event => update({ subtitleLanguages: event.target.checked ? [...options.subtitleLanguages ?? [], track.language] : options.subtitleLanguages?.filter(language => language !== track.language) })} /><span>{track.label}<small>{track.language}{track.automatic ? ' · 自动' : ''}</small></span></label>)}</div>}
        </section>
      </div>
    </details>
  </aside>
  return <div className="download-workspace">
    <PageHeader className="download-heading" title="下载" summary={activeCount ? `${activeCount} 个任务处理中，完成后可离线播放与阅读` : '把视频、音频和字幕保存到本机，随时离线继续。'} actions={<a className="button button-secondary" href="/tools#storage" aria-label="下载与存储设置" title="下载与存储设置"><Folder size={15}/>存储位置</a>}/>
    <section className="download-composer" aria-label="新建下载">
      <div className="download-composer-head">
        <h2>新建下载</h2>
        <span className={`download-tool-status ${downloads.tools?.downloadReady ? 'is-ready' : ''}`}><span aria-hidden="true"/>{downloads.tools?.downloadReady ? '下载工具已就绪' : downloads.loading ? '检查下载工具…' : '下载工具未就绪'}</span>
      </div>
      <form onSubmit={event => { event.preventDefault(); void downloads.inspect(url) }}>
        <label className="source-input"><Link2 size={17} aria-hidden="true" /><input aria-label="下载视频地址" placeholder="粘贴视频、音频或分享链接" value={url} onChange={event => { setURL(event.target.value); downloads.clearInfo() }} required maxLength={2048} /></label>
        <button className="button button-primary" disabled={!url.trim() || downloads.inspecting || !downloads.tools?.tools.some(tool => tool.name === 'yt-dlp' && tool.available)}>{downloads.inspecting ? <LoaderCircle className="spin" size={16} /> : <ArrowDownToLine size={16} />}{downloads.inspecting ? '获取中…' : '获取信息'}</button>
      </form>
      {downloads.infoError && <div className="error-panel" role="alert"><p>{downloads.infoError}</p><button className="button button-secondary" disabled={downloads.inspecting} onClick={() => void downloads.inspect(url)}><RefreshCw size={15} />重试</button></div>}
      {!downloads.loading && !downloads.tools?.downloadReady && <p className="inline-feedback"><CircleHelp size={17} />下载工具尚未准备好。<a href="/tools#storage">检查工具设置</a></p>}
      <div className="download-workbench">
        {info ? <section className="download-media" aria-label="媒体信息">
          <div className="download-preview">
            <div className="download-thumbnail">{info.thumbnailUrl && !thumbnailFailed ? <img src={info.thumbnailUrl} alt={info.title} referrerPolicy="no-referrer" onError={() => setThumbnailFailed(true)} /> : <>{videoFormats.length ? <Video size={32} strokeWidth={1.4} /> : <FileAudio size={32} strokeWidth={1.4} />}<span>暂无封面</span></>}{info.duration > 0 && <span className="download-duration">{duration(info.duration)}</span>}</div>
            <div className="download-media-copy"><span className="download-media-source">{info.extractor || new URL(info.url).hostname}</span><h2>{info.title}</h2>{info.author && <p>{info.author}</p>}<a href={info.url} target="_blank" rel="noopener noreferrer">打开原链接<ExternalLink size={13} /></a></div>
          </div>
          <div className="download-format-selection">
            {!audioOnly && <FormatChoices formats={videoFormats} label="视频格式" selected={options.videoFormatId ?? ''} onChange={id => { update({ videoFormatId: id }); setHeight(0) }} />}
            {(audioOnly || !options.videoOnly) && audioFormats.length > 0 && <FormatChoices formats={audioFormats} label="音频格式" selected={options.audioFormatId ?? ''} onChange={id => update({ audioFormatId: id })} />}
          </div>
        </section> : <section className="download-awaiting" aria-busy={downloads.inspecting} aria-label="媒体预览">
          {downloads.inspecting ? <LoaderCircle size={26} className="spin" /> : <Link2 size={26} strokeWidth={1.4} />}
          <h2>{downloads.inspecting ? '正在读取媒体信息' : '等待下载链接'}</h2>
          <p>{downloads.inspecting ? '正在获取画质、音轨和字幕' : '获取信息后，在这里核对标题并选择画质、音轨与字幕'}</p>
        </section>}
        {settingsPanel}
      </div>
      <div className="download-submit">
        <div><strong>{info ? `${audioOnly ? options.audioFormat === 'best' ? '原始音频' : options.audioFormat?.toUpperCase() : options.container?.toUpperCase()}${!audioOnly && options.videoOnly ? ' · 无音频' : ''}${!audioOnly && options.compress ? ' · 节省空间' : ''}` : '未选择媒体'}</strong><span>{info ? `${info.title}.${audioOnly ? options.audioFormat === 'best' ? '音频' : options.audioFormat : options.container}` : `保存到 ${downloads.tools?.downloadDir ?? '…'}`}{options.subtitleLanguages?.length ? ` · ${options.subtitleLanguages.length} 种字幕` : ''}</span></div>
        <button className="button button-primary" disabled={!ready || downloads.submitting} onClick={() => void start()}>{downloads.submitting ? <LoaderCircle className="spin" size={16} /> : <ArrowDownToLine size={16} />}开始下载</button>
      </div>
    </section>
    {downloads.error && <div className="error-panel" role="alert"><p>{downloads.error}</p><button className="button button-secondary" onClick={() => void downloads.reload()}>重新检查</button></div>}
    {downloads.notice && <p className="inline-feedback" role="status"><Check size={16} />{downloads.notice}</p>}
    <div className="download-history">
      <div className="section-heading download-history-toolbar"><h2 id="download-history-title" className="section-title">下载任务</h2>
        <div className="download-task-filters" role="group" aria-label="筛选下载任务">{taskFilters.map(({ value, label, count }) => <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value)}><span>{label}</span><span className="count" aria-hidden="true">{count}</span></button>)}</div>
        {deletable.length > 0 && <label className="download-select-all"><input type="checkbox" checked={allSelected} onChange={event => setSelected(event.target.checked ? new Set(deletable.map(job => job.id)) : new Set())} />全选</label>}
        {selected.size > 0 && <button className="button button-secondary" onClick={() => setDeleteDialog({ ids: [...selected], clear: false })}><Trash2 size={15}/>删除所选</button>}
        {downloads.jobs.some(job => job.state !== 'queued' && job.state !== 'running') && <button className="button button-secondary" onClick={() => setDeleteDialog({ ids: [], clear: true })}><Trash2 size={15}/>清空历史</button>}
        <button className="icon-button" title="刷新下载任务" aria-label="刷新下载任务" disabled={downloads.loading} onClick={() => void downloads.reload()}><RefreshCw size={16} className={downloads.loading ? 'spin' : undefined}/></button>
      </div>
      {downloads.loading ? <p className="loading-state" role="status">正在读取任务…</p> : jobs.length === 0 ? <section className="empty-state compact-empty"><ArrowDownToLine size={24} strokeWidth={1.4} aria-hidden="true" /><h2>{downloads.jobs.length ? '没有符合条件的任务' : '还没有下载任务'}</h2></section> : <section aria-label="下载任务" className="download-list">{jobs.map(job => <article className={`download-row state-${job.state}`} key={job.id}>
        {job.state !== 'queued' && job.state !== 'running' ? <label className="download-row-select"><input type="checkbox" checked={selected.has(job.id)} onChange={event => setSelected(current => { const next = new Set(current); if (event.target.checked) next.add(job.id); else next.delete(job.id); return next })} aria-label={`选择下载 ${job.title}`} /></label> : <span aria-hidden="true" />}
        <div className="download-state-icon" aria-hidden="true">{job.state === 'running' ? <LoaderCircle size={17} className="spin" /> : job.state === 'completed' ? <Check size={17} /> : job.mode === 'audio' ? <FileAudio size={17} /> : <Video size={17} />}</div>
        <div className="download-copy"><h3>{job.title}</h3><p><span className="download-state-label">{labels[job.state]}</span> <span>{job.mode === 'audio' ? job.audioFormat === 'best' || !job.audioFormat ? '音频' : job.audioFormat.toUpperCase() : `${job.container?.toUpperCase() ?? '视频'} · ${job.maxHeight ? `最高 ${job.maxHeight}p` : '原画'}`} · {formatDate(job.createdAt)}</span></p>{job.fileSize ? <p className="download-file-size">{bytes(job.fileSize)}{job.compressed && job.originalSize && job.originalSize > job.fileSize ? ` · 已节省 ${bytes(job.originalSize - job.fileSize)}` : ''}</p> : null}{job.message && <p className="download-message">{job.message}</p>}{(job.state === 'running' || job.state === 'queued') && <span className="download-progress" aria-hidden="true"/>}</div>
        <div className="download-actions">{(job.state === 'queued' || job.state === 'running') ? <button className="icon-button" title="取消下载" aria-label={`取消下载 ${job.title}`} onClick={() => void downloads.cancel(job.id)}><X size={17} /></button> : job.state === 'completed' ? <>{canRead(job) ? <button className="icon-button download-play" title="播放与阅读" data-content-id={`download:${job.id}`} aria-label={`在阅读页播放 ${job.title}`} onClick={() => onRead(job)}><Play size={16} /></button> : <a className="icon-button download-play" title="播放文件" aria-label={`播放文件 ${job.title}`} href={`/api/downloads/${encodeURIComponent(job.id)}/media`} target="_blank" rel="noopener noreferrer"><Play size={16} /></a>}<a className="icon-button" title={job.fileName ?? '保存文件'} aria-label="保存文件" href={`/api/downloads/${encodeURIComponent(job.id)}/file`} download><ArrowDownToLine size={17} /></a>{job.files?.filter(file => file !== job.fileName).map(file => <a className="icon-button" key={file} title={file} aria-label={`保存字幕 ${file}`} href={`/api/downloads/${encodeURIComponent(job.id)}/files/${encodeURIComponent(file)}`} download><Subtitles size={17} /></a>)}</> : <button className="icon-button" title="重新下载" disabled={downloads.submitting} onClick={() => void downloads.start(job.url, job.title, job.maxHeight, jobOptions(job))} aria-label={`重新下载 ${job.title}`}><RefreshCw size={17} /></button>}{job.state !== 'queued' && job.state !== 'running' && <button className="icon-button" title="删除记录" aria-label={`删除下载记录 ${job.title}`} onClick={() => setDeleteDialog({ ids: [job.id], clear: false })}><Trash2 size={16} /></button>}<a className="icon-button" title="打开原链接" href={job.url} rel="noopener noreferrer" target="_blank" aria-label={`打开下载原视频 ${job.title}`}><ExternalLink size={16} /></a></div>
      </article>)}</section>}
    </div>
    {deleteDialog && <div className="download-delete-dialog" role="dialog" aria-modal="true" aria-labelledby="download-delete-title"><div className="download-delete-panel"><h2 id="download-delete-title">{deleteDialog.clear ? '清空下载历史' : `删除 ${deleteDialog.ids.length} 条记录`}</h2><p>请选择是否同时删除已经下载到本机的文件。默认只删除记录。</p><div className="download-delete-actions"><button className="button button-secondary" onClick={() => setDeleteDialog(null)}>取消</button><button className="button button-secondary" onClick={() => void confirmDelete(false)}><Trash2 size={15}/>只删记录</button><button className="button button-danger" onClick={() => void confirmDelete(true)}><Trash2 size={15}/>记录和文件</button></div></div></div>}
  </div>
}
