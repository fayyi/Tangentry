import { Check, CircleHelp, Download, Folder, FolderOpen, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import { useCapabilities } from '../../shared/api/desktop'
import { listOrEmpty } from '../../shared/api/list'
import type { ToolStatus } from '../../shared/api/types'
import type { useDownloads } from '../downloads/useDownloads'

function toolProgress(tool: ToolStatus) {
  return tool.total ? `正在下载 ${Math.floor((tool.received ?? 0) * 100 / tool.total)}%` : '正在下载…'
}

export function StorageSettings({ downloads, onModelDirectoryChanged }: {
  downloads: ReturnType<typeof useDownloads>
  onModelDirectoryChanged: () => void
}) {
  const [pending, setPending] = useState(false), [error, setError] = useState('')
  const [directory, setDirectory] = useState<string | null>(null), [modelDirectory, setModelDirectory] = useState('')
  const { pathPicker } = useCapabilities()
  const [updating, setUpdating] = useState(false), [updateError, setUpdateError] = useState('')
  const tools = listOrEmpty(downloads.tools?.tools), toolDownloading = tools.some(tool => tool.downloading), { reload } = downloads
  useEffect(() => { if (!toolDownloading) return; const timer = window.setInterval(() => { if (!document.hidden) void reload() }, 1000); return () => window.clearInterval(timer) }, [toolDownloading, reload])
  useEffect(() => { if (!pathPicker) return; const controller = new AbortController(); void request<{ directory: string }>('/desktop/download-directory', { signal: controller.signal }).then(result => setDirectory(result.directory || null)).catch(() => {}); void request<{ directory: string }>('/desktop/model-directory', { signal: controller.signal }).then(result => setModelDirectory(result.directory || '')).catch(() => {}); return () => controller.abort() }, [pathPicker])
  async function update() { setUpdating(true); setUpdateError(''); try { await request('/tools/yt-dlp/update', { method: 'POST' }); await reload() } catch (cause) { setUpdateError(cause instanceof Error ? cause.message : '无法开始更新。') } finally { setUpdating(false) } }
  async function chooseDirectory(models: boolean) { setPending(true); setError(''); try { const result = await request<{ directory: string; cancelled: boolean }>(models ? '/desktop/model-directory' : '/desktop/download-directory', { method: 'POST' }); if (result.cancelled) return; if (models) { setModelDirectory(result.directory); onModelDirectoryChanged() } else { setDirectory(result.directory); await downloads.reload() } } catch (cause) { setError(cause instanceof Error ? cause.message : models ? '模型迁移失败。' : '无法选择下载目录。') } finally { setPending(false) } }
  return <>
    <section className="settings-section" aria-labelledby="storage-title"><div className="settings-heading"><div><h2 id="storage-title">文件位置</h2><p className="field-hint">管理下载文件与离线模型的保存位置。</p></div></div>
      <div className="settings-location-row"><Folder size={20} aria-hidden="true"/><div><strong>下载目录</strong><p className="settings-location-path">{pathPicker ? (directory || '尚未选择') : (downloads.tools?.downloadDir ?? '正在读取…')}</p>{!pathPicker && <p className="field-hint">这是 Tangentry 服务端上的路径，不是浏览器所在电脑的路径。</p>}</div>{pathPicker && <button type="button" className="button button-secondary" title="选择下载目录" aria-label="选择下载目录" disabled={pending} onClick={() => void chooseDirectory(false)}><FolderOpen size={15}/>{directory ? '重新选择' : '选择文件夹'}</button>}</div>
      <div className="settings-location-row"><Folder size={20} aria-hidden="true"/><div><strong>模型存储目录</strong><p className="settings-location-path">{modelDirectory || (pathPicker ? '正在读取…' : '由 Tangentry 服务端配置')}</p><p className="field-hint">{pathPicker ? (pending ? '正在处理，请稍候…' : '更改位置时自动迁移已下载模型。') : '这是 Tangentry 服务端上的路径，不是浏览器所在电脑的路径。'}</p></div>{pathPicker && <button type="button" className="button button-secondary" title="选择并迁移模型目录" aria-label="选择并迁移模型目录" disabled={pending} onClick={() => void chooseDirectory(true)}><FolderOpen size={15}/>{modelDirectory ? '重新选择' : '选择文件夹'}</button>}</div>
      {error && <p className="subtitle-error" role="alert">{error}</p>}
    </section>
    <section className="settings-section" aria-labelledby="tools-title"><div className="settings-heading"><div><h2 id="tools-title">下载工具</h2><p className="field-hint">更改系统工具后，请重启。</p></div><button className="icon-button" aria-label="刷新状态" title="刷新状态" disabled={downloads.loading} onClick={() => void downloads.reload()}><RefreshCw size={16} className={downloads.loading ? 'spin' : undefined}/></button></div>
      {downloads.loading && <p className="field-hint" role="status">正在检查工具…</p>}{downloads.error && <p className="subtitle-error" role="alert">{downloads.error}</p>}
      {tools.map(tool => <div className="setting-row settings-tool-row" key={tool.name}><div><strong>{tool.name}</strong><p>{tool.name === 'yt-dlp' ? '获取视频与已有字幕' : '合并音视频与媒体处理'}</p>{tool.managed && !tool.installed && !tool.downloading && !tool.error && <p className="field-hint">首次使用时自动下载并校验。</p>}{tool.error && <p className="subtitle-error" role="alert">{tool.error}</p>}</div><div className="tool-value">{tool.downloading ? <span className="settings-state" role="status"><RefreshCw size={14} className="spin"/> {toolProgress(tool)}</span> : <span className="settings-state" data-ready={tool.available}>{tool.available ? <Check size={14}/> : <CircleHelp size={14}/>} {tool.available ? '已就绪' : '未检测到'}</span>}{tool.version && <small>{tool.version.replace(/^ffmpeg version /, '').split(' Copyright')[0]}</small>}{tool.managed && <button type="button" className="button button-secondary" disabled={tool.downloading || updating} onClick={() => void update()}><Download size={14}/>{tool.installed ? '更新 yt-dlp' : '下载 yt-dlp'}</button>}</div></div>)}
      {updateError && <p className="subtitle-error" role="alert">{updateError}</p>}
    </section>
  </>
}
