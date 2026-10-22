import { Check, FolderSync, LoaderCircle, Plus, RefreshCw, Settings2, Trash2, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { CardCollection } from '../../shared/ui/CardCollection'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import { useFavoriteSync } from './useFavoriteSync'
import type { FavoritePolicy, FavoriteSource } from './useFavoriteSync'
import { parseSubtitleLanguages, sourcePolicyDraft, subtitleLanguageText, syncPolicyDraft } from './policyDraft'
import './favorites.css'

type FavoriteState = ReturnType<typeof useFavoriteSync>
export type LibrarySource = 'local' | 'setup' | 'folder'

function PolicyEditor({ source, disabled, onSave }: { source: FavoriteSource; disabled: boolean; onSave: (policy: FavoritePolicy) => Promise<boolean> }) {
  const [draft, setDraft] = useState<FavoritePolicy>(() => sourcePolicyDraft(source.policy))
  const [subtitleText, setSubtitleText] = useState(() => subtitleLanguageText(source.policy.download))
  const [notice, setNotice] = useState('')
  const [dirty, setDirty] = useState(false)
  const updateDraft = (update: (value: FavoritePolicy) => FavoritePolicy) => { setDraft(update); setDirty(true); setNotice('') }
  useEffect(() => {
    if (dirty) return
    setDraft(current => syncPolicyDraft(current, source.policy, dirty))
    setSubtitleText(subtitleLanguageText(source.policy.download))
  }, [dirty, source.policy])
  return <form className="favorite-policy" aria-label="收藏同步设置" onSubmit={event => { event.preventDefault(); void onSave(draft).then(ok => { if (ok) { setDirty(false); setNotice('同步设置已保存。') } }) }}>
    <div className="favorite-switches">
      <label><input type="checkbox" checked={draft.enabled} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, enabled: event.target.checked }))} /><span><strong>定时同步这个收藏夹</strong><small>按间隔读取 B站收藏，新收藏会出现在这里</small></span></label>
      <label><input type="checkbox" checked={draft.autoDownload} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, autoDownload: event.target.checked }))} /><span><strong>自动下载新增视频</strong><small>关闭时只同步收藏列表，不占用磁盘</small></span></label>
    </div>
    <div className="favorite-policy-fields"><label>同步间隔<select aria-label="收藏同步间隔" disabled={disabled} value={draft.intervalMinutes} onChange={event => updateDraft(value => ({ ...value, intervalMinutes: Number(event.target.value) }))}>{[15, 60, 360, 1440].map(value => <option key={value} value={value}>{value < 60 ? `${value} 分钟` : `${value / 60} 小时`}</option>)}</select></label>
      <label>每轮最多下载<select aria-label="每轮最多下载" disabled={disabled} value={draft.maxPerRun} onChange={event => updateDraft(value => ({ ...value, maxPerRun: Number(event.target.value) }))}>{[1, 3, 5].map(value => <option key={value} value={value}>{value} 个视频</option>)}</select></label>
      <label>最高画质<select aria-label="收藏下载最高画质" disabled={disabled} value={draft.maxHeight} onChange={event => updateDraft(value => ({ ...value, maxHeight: Number(event.target.value) }))}><option value={0}>原画</option>{[480, 720, 1080, 1440, 2160].map(value => <option key={value} value={value}>{value}p</option>)}</select></label>
      <label>封装格式<select aria-label="收藏视频封装格式" disabled={disabled} value={draft.download.container} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, container: event.target.value as 'mp4' | 'mkv' } }))}><option value="mp4">MP4</option><option value="mkv">MKV</option></select></label>
    </div>
    <div className="favorite-download-toggles" role="group" aria-label="收藏视频处理选项">
      <label><input type="checkbox" checked={!!draft.download.compress} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, compress: event.target.checked } }))}/><span>节省空间</span></label>
      <label><input type="checkbox" checked={!draft.download.videoOnly} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, videoOnly: !event.target.checked } }))}/><span>保留音频</span></label>
      <label><input type="checkbox" checked={draft.downloadSubtitles} disabled={disabled} onChange={event => { setSubtitleText(''); updateDraft(value => ({ ...value, downloadSubtitles: event.target.checked, download: { ...value.download, subtitleLanguages: event.target.checked ? ['all'] : [], automaticSubtitles: event.target.checked && value.download.automaticSubtitles } })) }}/><span>下载字幕</span></label>
      <label><input type="checkbox" checked={!!draft.download.automaticSubtitles} disabled={disabled || !draft.downloadSubtitles} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, automaticSubtitles: event.target.checked } }))}/><span>包含自动字幕</span></label>
      <label><input type="checkbox" checked={!!draft.download.downloadDanmaku} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, downloadDanmaku: event.target.checked } }))}/><span>下载弹幕</span></label>
      <label><input type="checkbox" checked={!!draft.download.downloadThumbnail} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, downloadThumbnail: event.target.checked } }))}/><span>下载封面</span></label>
      <label><input type="checkbox" checked={!!draft.download.downloadInfoJSON} disabled={disabled} onChange={event => updateDraft(value => ({ ...value, download: { ...value.download, downloadInfoJSON: event.target.checked } }))}/><span>保存视频信息</span></label>
    </div>
    <label className="favorite-directory-name">下载文件夹名称<input value={draft.directoryName ?? ''} maxLength={80} disabled={disabled} placeholder={`${source.title} (${source.folderId})`} onChange={event => updateDraft(value => ({ ...value, directoryName: event.target.value }))} /><small>保存后只影响新下载；旧视频和字幕保留在原目录。</small></label>
    {draft.downloadSubtitles && <label className="favorite-subtitle-languages">字幕语言<input value={subtitleText} disabled={disabled} placeholder="留空时下载所有人工字幕" onChange={event => {
      const text = event.target.value
      setSubtitleText(text)
      updateDraft(value => ({ ...value, download: { ...value.download, subtitleLanguages: parseSubtitleLanguages(text) } }))
    }}/><small>用逗号分隔，例如 zh-Hans, en；开启自动字幕后也会读取对应的自动字幕。</small></label>}
    <div className="favorite-download-filters" role="group" aria-label="自动下载规则">
      <label>标题包含<input value={draft.titleInclude ?? ''} maxLength={300} disabled={disabled} placeholder="例如：教程，访谈" onChange={event => updateDraft(value => ({ ...value, titleInclude: event.target.value }))} /></label>
      <label>标题排除<input value={draft.titleExclude ?? ''} maxLength={300} disabled={disabled} placeholder="例如：预告，直播回放" onChange={event => updateDraft(value => ({ ...value, titleExclude: event.target.value }))} /></label>
      <label>作者包含<input value={draft.authorInclude ?? ''} maxLength={300} disabled={disabled} placeholder="留空不限作者" onChange={event => updateDraft(value => ({ ...value, authorInclude: event.target.value }))} /></label>
    </div>
    <p className="field-hint">目录为「下载位置 / B站收藏夹 / 收藏夹名称 / 视频标题 [BV号]」，视频、字幕、弹幕、封面和视频信息放在同一目录。下载数量、格式、画质、附属内容和关键词只在开启自动下载时生效。多个关键词用逗号分隔；标题与作者条件需同时满足，排除优先。移除远端条目会保留本机文件和笔记。</p>
    <div className="favorite-policy-actions"><button className="button button-primary" disabled={disabled}>保存同步设置</button>{notice && <p className="inline-feedback" role="status"><Check size={14}/>{notice}</p>}</div>
  </form>
}

// Source tabs above the library: local saves first, then every synced B站 folder.
export function LibrarySources({ favorites, mode, localCount, onLocal, onFolder, onSetup }: { favorites: FavoriteState; mode: LibrarySource; localCount: number; onLocal: () => void; onFolder: (id: string) => void; onSetup: () => void }) {
  return <nav className="library-sources" aria-label="收藏来源">
    <button type="button" aria-pressed={mode === 'local'} onClick={onLocal}>本机收藏<span className="count">{localCount}</span></button>
    {favorites.sources.map(source => <button key={source.folderId} type="button" aria-pressed={mode === 'folder' && favorites.selected === source.folderId} onClick={() => onFolder(source.folderId)} title={source.policy.autoDownload ? '同步并自动下载' : '仅同步收藏，不下载'}>
      <FolderSync size={14} aria-hidden="true"/>{source.title}<span className="count">{source.itemCount}</span>{source.active && <LoaderCircle size={13} className="spin" aria-label="同步中"/>}
    </button>)}
    <button type="button" className="library-source-add" aria-pressed={mode === 'setup'} onClick={onSetup}><Plus size={14} aria-hidden="true"/>同步 B站收藏夹</button>
  </nav>
}

export function FavoriteSetup({ favorites, onFollowed }: { favorites: FavoriteState; onFollowed: (id: string) => void }) {
  const [downloads, setDownloads] = useState<Record<string, boolean>>({})
  const [query, setQuery] = useState('')
  const [manualDownload, setManualDownload] = useState(false)
  const blocked = favorites.pending || favorites.loading
  const synced = new Set(favorites.sources.map(source => source.folderId))
  useEffect(() => { if (!favorites.folders.length && !favorites.readingFolders && !favorites.folderError) void favorites.readFolders() }, [])
  const follow = async (id: string, download: boolean) => { const folder = await favorites.follow(id, download); if (folder) onFollowed(folder) }
  return <section className="favorite-setup" aria-label="B站收藏同步">
    <div className="favorite-setup-heading"><div><h2>同步 B站收藏夹</h2><p className="field-hint">选择收藏夹后会立刻同步，新的收藏会出现在「收藏」里。默认只同步列表；需要离线观看时，再勾选下载。</p></div><button type="button" className="button button-secondary" disabled={favorites.readingFolders || blocked} onClick={() => void favorites.readFolders()}>{favorites.readingFolders ? <LoaderCircle className="spin" size={15}/> : <RefreshCw size={15}/>}读取我的收藏夹</button></div>
    {favorites.folderError && <p className="inline-feedback" role="alert">{favorites.folderError}</p>}
    {favorites.error && <p className="inline-feedback" role="alert">{favorites.error}</p>}
    {favorites.folders.length > 0 && <ul className="favorite-folder-list" aria-label="我的 B站收藏夹">{favorites.folders.map(folder => {
      const followed = synced.has(folder.id)
      return <li key={folder.id}>
        <div className="favorite-folder-name"><strong>{folder.title}</strong><span>{folder.count} 个内容</span></div>
        {!followed && <label className="favorite-folder-download"><input type="checkbox" checked={downloads[folder.id] === true} disabled={blocked} onChange={event => setDownloads(value => ({ ...value, [folder.id]: event.target.checked }))}/>同时下载</label>}
        {followed ? <button type="button" className="button button-secondary" onClick={() => { favorites.setSelected(folder.id); onFollowed(folder.id) }}><Check size={14}/>已同步 · 查看</button>
          : <button type="button" className="button button-primary" aria-label={`收藏并启用同步：${folder.title}`} disabled={blocked} onClick={() => void follow(folder.id, downloads[folder.id] === true)}>{favorites.pending ? <LoaderCircle size={14} className="spin"/> : <FolderSync size={14}/>}收藏并启用同步</button>}
      </li>
    })}</ul>}
    {favorites.readingFolders && !favorites.folders.length && <p className="field-hint" role="status"><LoaderCircle size={14} className="spin"/>正在读取收藏夹…</p>}
    <details className="favorite-manual">
      <summary>通过收藏夹地址或 ID 添加</summary>
      <form className="favorite-add" onSubmit={event => { event.preventDefault(); void follow(query, manualDownload).then(() => setQuery('')) }}>
        <label>收藏夹地址或 ID<input aria-label="收藏夹地址或 ID" value={query} onChange={event => setQuery(event.target.value)} maxLength={2048} placeholder="space.bilibili.com 收藏夹地址，或 fid 数字" required /></label>
        <label className="favorite-folder-download"><input type="checkbox" checked={manualDownload} disabled={blocked} onChange={event => setManualDownload(event.target.checked)}/>同时下载</label>
        <button className="button button-primary" disabled={blocked || !query.trim()}>收藏并启用同步</button>
      </form>
    </details>
  </section>
}

export function FavoriteFolder({ favorites, savedIDs, savingUnavailable, savingIDs, onSave, onRead, onDownload, onRemoved }: { favorites: FavoriteState; savedIDs: Set<string>; savingUnavailable: boolean; savingIDs: Set<string>; onSave: (item: ContentItem) => void; onRead: (item: ContentItem) => void; onDownload: (item: ContentItem) => void; onRemoved: () => void }) {
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [settings, setSettings] = useState(false)
  const [shown, setShown] = useState(40)
  const source = favorites.source
  useEffect(() => { setConfirmRemove(false); setSettings(false); setShown(40) }, [favorites.selected])
  if (!source) return favorites.loading ? <p className="field-hint" role="status"><LoaderCircle size={14} className="spin"/>正在读取收藏同步…</p> : null
  const blocked = favorites.pending || favorites.loading || favorites.active
  const items = favorites.detail?.folderId === source.folderId ? favorites.detail.items : []
  const synced = source.lastSuccessAt ? `最近同步 ${new Date(source.lastSuccessAt * 1000).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}` : '尚未同步内容'
  return <section className="favorite-folder" aria-label="B站收藏同步">
    <div className="favorite-detail-heading">
      <div><h2>{source.title}</h2><p className="field-hint">{source.itemCount} 个视频 · {synced}{source.policy.enabled ? ` · 每 ${source.policy.intervalMinutes < 60 ? `${source.policy.intervalMinutes} 分钟` : `${source.policy.intervalMinutes / 60} 小时`}同步` : ' · 手动同步'} · {source.policy.autoDownload ? '自动下载新增' : '仅同步，不下载'}{source.pendingCount > 0 && ` · ${source.pendingCount} 个视频等待后续轮次下载`}</p></div>
      <div className="favorite-detail-actions">
        <button type="button" className="button button-secondary" disabled={favorites.pending || (favorites.active && !source.active)} onClick={() => void (source.active ? favorites.cancel(source.folderId) : favorites.start(source.folderId))}><RefreshCw size={15} className={source.active ? 'spin' : ''}/>{source.active ? '取消收藏同步' : '立即同步收藏夹'}</button>
        <button type="button" className="icon-button" aria-label="同步设置" title="同步设置" aria-expanded={settings} onClick={() => setSettings(value => !value)}><Settings2 size={16}/></button>
        <button type="button" className="icon-button" aria-label="移除同步配置" title="移除同步配置" disabled={blocked} onClick={() => setConfirmRemove(value => !value)}><Trash2 size={16}/></button>
      </div>
    </div>
    {(source.message || favorites.notice) && <p className="inline-feedback" role="status">{favorites.notice || source.message}</p>}
    {favorites.error && <p className="inline-feedback" role="alert">{favorites.error}<button type="button" className="button button-secondary" disabled={favorites.pending} onClick={() => void favorites.reload()}>刷新收藏同步</button></p>}
    {confirmRemove && <div className="favorite-remove"><p>移除这个同步来源？本机下载、收藏和笔记会保留。</p><button type="button" className="button button-secondary" onClick={() => setConfirmRemove(false)}><X size={14}/>保留同步配置</button><button type="button" className="button button-primary" disabled={blocked} onClick={() => void favorites.remove(source.folderId).then(ok => { if (ok) onRemoved() })}>确认移除同步配置</button></div>}
    {settings && <PolicyEditor key={source.folderId} source={source} disabled={blocked} onSave={policy => favorites.configure(source.folderId, policy)} />}
    {favorites.detailError && <p className="inline-feedback" role="alert">{favorites.detailError}</p>}
    {items.length > 0 ? <>
      <CardCollection masonry={false} data-media="video" className="content-collection gallery-layout modern-gallery" aria-label={`B站收藏夹 ${source.title}`}>{items.slice(0, shown).map(item => <ModernGalleryCard key={item.id} item={item} saved={savedIDs.has(item.id)} pending={savingUnavailable || savingIDs.has(item.id)} onSave={onSave} onRead={onRead} onDownload={onDownload}/>)}</CardCollection>
      {items.length > shown && <div className="load-more"><button type="button" className="button button-secondary" onClick={() => setShown(value => value + 40)}>显示更多收藏内容</button></div>}
    </> : !source.active && <section className="empty-state"><h2>{source.lastSuccessAt ? '这个收藏夹是空的' : '还没有同步内容'}</h2><p>{source.lastSuccessAt ? '在 B站收藏视频后，下次同步就会出现在这里。' : '点击「立即同步收藏夹」读取 B站里的收藏。'}</p></section>}
  </section>
}
