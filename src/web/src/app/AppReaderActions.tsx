import { Button } from '../components/ui'
import { AlertCircle, Bookmark, Check, Download } from 'lucide-react'
import type { ContentItem } from '../shared/api/types'
import { ReaderActions } from '../shared/reader/ReaderActions'
import { ToolbarPopover } from '../shared/ui/ToolbarPopover'

type ReadState = {
  ready: boolean
  pending: Set<string>
  error: string | null
  get: (id: string) => { read: boolean } | undefined
  setRead: (ids: string[], read: boolean) => Promise<unknown>
  refresh: () => void
}
type LibraryState = {
  pending: Set<string>
  loading: boolean
  error: string | null
  notice: string | null
  noticeError: boolean
  toggle: (item: ContentItem) => Promise<unknown>
  reload: () => Promise<unknown>
}

type Props = {
  item: ContentItem | null
  saved: boolean
  savingUnavailable: boolean
  libraryPending: Set<string>
  library: LibraryState
  downloads: { submitting: boolean; loading: boolean; error: string | null }
  readState: ReadState
  readingID: string
  supportsReadState: boolean
  onDownload: (item: ContentItem) => void
}

export function AppReaderActions({ item, saved, savingUnavailable, libraryPending, library, downloads, readState, readingID, supportsReadState: canRead, onDownload }: Props) {
  if (!item && !canRead) return null
  return <ReaderActions><div className="reader-read-state">
    {item && <Button variant="secondary" className="button button-secondary" title={saved ? '取消收藏内容' : '收藏内容'} aria-pressed={saved} disabled={savingUnavailable || libraryPending.has(item.id)} onClick={() => void library.toggle(item)}>
      <Bookmark size={14} aria-hidden="true" />{saved ? '取消收藏内容' : '收藏内容'}
    </Button>}
    {item && ((item.kind === 'video' && ['bilibili', 'youtube', 'douyin', 'xiaohongshu'].includes(item.platform)) || item.kind === 'audio' && item.mediaUrl) && <Button variant="secondary" className="button button-secondary" title={item.kind === 'audio' ? '下载音频' : '下载视频'} disabled={downloads.submitting || downloads.loading} onClick={() => onDownload(item)}><Download size={14} aria-hidden="true" />{item.kind === 'audio' ? '下载音频' : '下载视频'}</Button>}
    {downloads.error && item?.kind === 'video' && <ToolbarPopover label="下载失败" title="查看下载错误" tone="danger" icon={<AlertCircle size={15} aria-hidden="true" />} announcement={downloads.error}><p role="alert">{downloads.error}</p></ToolbarPopover>}
    {canRead && <Button variant="secondary" className="button button-secondary" title={readState.get(readingID)?.read ? '标为未读' : '标为已读'} disabled={!readState.ready || readState.pending.has(readingID)} onClick={() => void readState.setRead([readingID], !readState.get(readingID)?.read)}>
      <Check size={14} aria-hidden="true" />{readState.get(readingID)?.read ? '标为未读' : '标为已读'}
    </Button>}
    {library.notice && <ToolbarPopover label={library.noticeError ? '收藏操作失败' : '收藏已更新'} title={library.notice} tone={library.noticeError ? 'danger' : 'success'} icon={<Check size={15} aria-hidden="true" />} announcement={library.notice}><p role={library.noticeError ? 'alert' : 'status'}>{library.notice}</p></ToolbarPopover>}
    {library.error && <ToolbarPopover label="收藏状态异常" title="查看收藏错误" tone="danger" icon={<AlertCircle size={15} aria-hidden="true" />} announcement={library.error}><p role="alert">{library.error}</p><Button variant="secondary" className="button button-secondary" disabled={library.loading} onClick={() => void library.reload()}>重新读取收藏</Button></ToolbarPopover>}
    {readState.error && <ToolbarPopover label="已读状态异常" title="查看已读状态错误" tone="danger" icon={<AlertCircle size={15} aria-hidden="true" />} announcement={readState.error}><p role="alert">{readState.error}</p><Button variant="secondary" className="button button-secondary" disabled={readState.pending.size > 0} onClick={readState.refresh}>重新读取已读状态</Button></ToolbarPopover>}
  </div></ReaderActions>
}
