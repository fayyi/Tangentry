export interface BackupRemoteFolder {
  name: string
  path: string
}

export function backupFolderBrowseRequest<T extends object>(target: T, folderPath: string) {
  return { ...target, type: 'webdav', folderPath }
}

export type FolderBrowserStatus = 'idle' | 'loading' | 'ready' | 'error'
export type FolderCreateStatus = 'idle' | 'creating' | 'created' | 'exists' | 'error'

export interface BackupFolderBrowserState {
  path: string
  folders: BackupRemoteFolder[]
  status: FolderBrowserStatus
  createStatus: FolderCreateStatus
  error: string
  createError: string
  folderName: string
}

export type BackupFolderBrowserEvent =
  | { type: 'load-started'; path: string }
  | { type: 'load-succeeded'; folders: BackupRemoteFolder[] }
  | { type: 'load-failed'; message: string }
  | { type: 'create-name-changed'; name: string }
  | { type: 'create-started' }
  | { type: 'create-succeeded'; exists: boolean }
  | { type: 'create-failed'; message: string }

export function createBackupFolderBrowserState(): BackupFolderBrowserState {
  return { path: '', folders: [], status: 'idle', createStatus: 'idle', error: '', createError: '', folderName: '' }
}

export function backupFolderBrowserTransition(state: BackupFolderBrowserState, event: BackupFolderBrowserEvent): BackupFolderBrowserState {
  switch (event.type) {
    case 'load-started':
      return { ...state, path: event.path, folders: [], status: 'loading', error: '', createStatus: state.createStatus === 'creating' ? 'creating' : 'idle', createError: state.createStatus === 'creating' ? state.createError : '' }
    case 'load-succeeded':
      return { ...state, folders: event.folders, status: 'ready', error: '' }
    case 'load-failed':
      return { ...state, folders: [], status: 'error', error: event.message }
    case 'create-name-changed':
      return { ...state, folderName: event.name, createStatus: 'idle', createError: '' }
    case 'create-started':
      return { ...state, createStatus: 'creating', createError: '' }
    case 'create-succeeded':
      return { ...state, createStatus: event.exists ? 'exists' : 'created', createError: '' }
    case 'create-failed':
      return { ...state, createStatus: 'error', createError: event.message }
  }
}

export function backupFolderParent(path: string): string {
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.join('/')
}

export function selectedBackupFolderPath(state: BackupFolderBrowserState): string {
  return state.path
}

export function backupFolderBreadcrumbs(path: string): Array<{ name: string; path: string }> {
  const parts = path.split('/').filter(Boolean)
  return [{ name: '根目录', path: '' }, ...parts.map((name, index) => ({ name, path: parts.slice(0, index + 1).join('/') }))]
}

export function normalizeBackupSettingsData<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : []
}
