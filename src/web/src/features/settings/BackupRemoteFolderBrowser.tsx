import { ArrowLeft, Check, ChevronRight, Folder, FolderPlus, LoaderCircle, RefreshCw, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { APIError, request } from '../../shared/api/client'
import type { BackupTarget } from '../../shared/api/types'
import { backupFolderBreadcrumbs, backupFolderBrowseRequest, backupFolderBrowserTransition, backupFolderParent, createBackupFolderBrowserState, selectedBackupFolderPath, type BackupRemoteFolder } from './backupFolderBrowser'

interface Props {
  target: Pick<BackupTarget, 'id' | 'type' | 'endpoint' | 'username' | 'remotePath' | 'password' | 'allowInsecureTLS'>
  onSelect(path: string): void
  onClose(): void
}

export function BackupRemoteFolderBrowser({ target, onSelect, onClose }: Props) {
  const [state, setState] = useState(createBackupFolderBrowserState)
  const loadID = useRef(0)
  const browseController = useRef<AbortController | null>(null)

  async function load(path: string) {
    browseController.current?.abort()
    const requestID = ++loadID.current
    const controller = new AbortController()
    browseController.current = controller
    const timeout = window.setTimeout(() => controller.abort(), 25000)
    setState(current => backupFolderBrowserTransition(current, { type: 'load-started', path }))
    try {
      const folders = await request<BackupRemoteFolder[]>('/backup/targets/browse', { method: 'POST', body: JSON.stringify(backupFolderBrowseRequest(target, path)), signal: controller.signal })
      if (requestID !== loadID.current) return
      setState(current => backupFolderBrowserTransition(current, { type: 'load-succeeded', folders: Array.isArray(folders) ? folders : [] }))
    } catch (cause) {
      if (requestID !== loadID.current) return
      if (controller.signal.aborted) {
        setState(current => backupFolderBrowserTransition(current, { type: 'load-failed', message: '读取远端目录超时，请检查连接后重试。' }))
        return
      }
      const message = cause instanceof APIError
        ? cause.message
        : cause instanceof Error
          ? cause.message
          : '读取远端目录失败，请检查权限和连接后重试。'
      setState(current => backupFolderBrowserTransition(current, { type: 'load-failed', message }))
    } finally {
      window.clearTimeout(timeout)
      if (requestID === loadID.current) browseController.current = null
    }
  }

  useEffect(() => {
    void load('')
    return () => {
      ++loadID.current
      browseController.current?.abort()
      browseController.current = null
    }
  }, [])

  async function createFolder() {
    const name = state.folderName.trim()
    if (!name || state.createStatus === 'creating') return
    setState(current => backupFolderBrowserTransition(current, { type: 'create-started' }))
    try {
      const result = await request<{ status: 'created' | 'exists' }>('/backup/targets/mkdir', {
        method: 'POST', body: JSON.stringify({ ...target, folderPath: state.path, folderName: name }),
      })
      setState(current => backupFolderBrowserTransition(current, { type: 'create-succeeded', exists: result.status === 'exists' }))
      void load(state.path)
    } catch (cause) {
      setState(current => backupFolderBrowserTransition(current, { type: 'create-failed', message: cause instanceof Error ? cause.message : '新建文件夹失败，请检查权限后重试。' }))
    }
  }

  const breadcrumbs = backupFolderBreadcrumbs(state.path)
  const parent = backupFolderParent(state.path)

  return <section className="backup-folder-browser" aria-label="选择 WebDAV 远端目录">
    <div className="backup-folder-browser-heading">
      <div><h4>选择远端目录</h4><p>选择备份保存位置。</p></div>
      <button type="button" className="icon-button" title="关闭目录浏览" aria-label="关闭目录浏览" onClick={onClose}><X size={16}/></button>
    </div>
    <nav className="backup-folder-breadcrumbs" aria-label="远端目录路径">
      {breadcrumbs.map((crumb, index) => <span key={crumb.path || 'root'}>
        {index > 0 && <ChevronRight size={14} aria-hidden="true"/>}
        <button type="button" className="text-button" aria-current={index === breadcrumbs.length - 1 ? 'location' : undefined} onClick={() => void load(crumb.path)} disabled={state.status === 'loading' || state.createStatus === 'creating'}>{crumb.name}</button>
      </span>)}
    </nav>
    <div className="backup-folder-list" role="group" aria-label="子文件夹" aria-busy={state.status === 'loading'}>
      {state.status === 'loading' && <p className="backup-folder-state" role="status"><LoaderCircle size={16} className="spin"/>正在读取文件夹…</p>}
      {state.status === 'error' && <div className="backup-folder-state backup-folder-error" role="alert"><p>{state.error}</p><button type="button" className="button button-secondary" onClick={() => void load(state.path)}><RefreshCw size={15}/>重试</button></div>}
      {state.status === 'ready' && state.path !== '' && <button type="button" className="backup-folder-row" disabled={state.createStatus === 'creating'} onClick={() => void load(parent)}><ArrowLeft size={16}/><span>返回上级</span></button>}
      {state.status === 'ready' && state.folders.map(folder => <button type="button" className="backup-folder-row" key={folder.path} disabled={state.createStatus === 'creating'} onClick={() => void load(folder.path)}><Folder size={16}/><span>{folder.name}</span><ChevronRight size={15}/></button>)}
      {state.status === 'ready' && state.folders.length === 0 && <p className="backup-folder-state">此目录下没有子文件夹。</p>}
    </div>
    <form className="backup-folder-create" onSubmit={event => { event.preventDefault(); void createFolder() }}>
      <label htmlFor="backup-folder-name"><FolderPlus size={16}/>新建文件夹</label>
      {state.path === '' && <p className="backup-folder-hint">根目录建议新建 Tangentry，系统不会自动创建。</p>}
      <div><input id="backup-folder-name" value={state.folderName} maxLength={120} placeholder={state.path === '' ? 'Tangentry' : '文件夹名称'} disabled={state.createStatus === 'creating'} onChange={event => setState(current => backupFolderBrowserTransition(current, { type: 'create-name-changed', name: event.target.value }))}/><button type="submit" className="button button-secondary" disabled={!state.folderName.trim() || state.createStatus === 'creating'}>{state.createStatus === 'creating' ? '创建中…' : '创建'}</button></div>
      {state.createStatus === 'created' && <p className="backup-folder-success" role="status"><Check size={14}/>文件夹已创建。</p>}
      {state.createStatus === 'exists' && <p className="backup-folder-state" role="status">该文件夹已存在，可以直接进入或选择。</p>}
      {state.createStatus === 'error' && <p className="backup-folder-error" role="alert">{state.createError}</p>}
    </form>
    <div className="backup-folder-footer"><span>当前目录：<strong>{state.path || '根目录'}</strong></span><button type="button" className="button button-secondary" disabled={state.status !== 'ready'} onClick={() => onSelect(selectedBackupFolderPath(state))}>选择此文件夹</button></div>
  </section>
}
