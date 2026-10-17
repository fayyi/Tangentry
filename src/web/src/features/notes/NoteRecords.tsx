import { Camera, Check, Clock3, Copy, ExternalLink, Pencil, Quote, Trash2 } from 'lucide-react'
import { useRef, useState } from 'react'
import type { ReadingNote, SubtitleSourceVersion } from '../../shared/api/types'
import { noteOriginalAt } from './source'
import { formatPosition } from './noteTime'
import { NoteBody } from './NoteBody'
import { NoteFrame } from './NoteFrame'
import { notePresentation } from './notePresentation'
import { SubtitleReferences } from './SubtitleReferences'

export function NoteRecords({ notes, contentId, sourceVersion, showTime, unavailable, hasDraft, editingId, copiedId, activeNoteIds, linkedHighlight, unresolvedAnchorIds, captureScreen, capturePending, selectedTag, getDownloadID, onSeek, onJumpText, onJumpPDF, onCapture, onCaptureNote, onCaptureCurrent, onRemoveFrame, onCopy, onEdit, onRemove, onTaskChange, onTagChange, onNotice, onDeleted }: {
  notes: ReadingNote[]
  contentId: string
  sourceVersion: SubtitleSourceVersion | null
  showTime: boolean
  unavailable: boolean
  hasDraft: boolean
  editingId: string | null
  copiedId: string | null
  activeNoteIds: string[]
  linkedHighlight: string
  unresolvedAnchorIds: string[]
  captureScreen?: boolean
  capturePending?: boolean
  selectedTag?: string
  getDownloadID?: () => string | null
  onSeek?: (position: number) => boolean | void
  onJumpText?: (id: string) => void
  onJumpPDF?: (id: string) => void
  onCapture?: (note: ReadingNote, seconds: number) => void
  onCaptureNote?: (note: ReadingNote) => void
  onCaptureCurrent?: (note: ReadingNote) => void
  onRemoveFrame?: (note: ReadingNote, frameId: string) => void
  onCopy: (note: ReadingNote) => void
  onEdit: (note: ReadingNote) => void
  onRemove: (note: ReadingNote, expectedUpdatedAt: number) => Promise<boolean>
  onTaskChange: (note: ReadingNote, offset: number, checked: boolean) => void
  onTagChange: (tag: string) => void
  onNotice: (message: string) => void
  onDeleted?: () => void
}) {
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const deleteVersion = useRef(0)
  if (notes.length === 0) return null
  return <ol className="note-list" aria-label="已保存笔记">{notes.map(note => {
    const presentation = notePresentation(note)
    const active = activeNoteIds.includes(note.id) || linkedHighlight === note.id
    const editing = editingId === note.id
    return <li key={note.id} id={'reading-note-' + note.id} tabIndex={-1} data-reading-note-id={note.id} data-active={active} data-editing={editing}>
      <div className="note-record-heading"><div className="note-meta"><NoteLocation note={note} contentId={contentId} showTime={showTime} unresolved={unresolvedAnchorIds.includes(note.id)} onSeek={onSeek} onJumpText={onJumpText} onJumpPDF={onJumpPDF} onNotice={onNotice}/></div></div>
      <SubtitleReferences references={note.subtitleReferences} contentId={contentId} sourceVersion={sourceVersion} onSeek={onSeek ? position => { if (onSeek(position) === false) onNotice('关联时间超出当前媒体范围，暂无法跳转。') } : undefined}/>
      <NoteFrame note={note} onSeek={onSeek ? position => { if (onSeek(position) === false) onNotice('关联时间超出当前媒体范围，暂无法跳转。') } : undefined} disabled={unavailable || editing} onCapture={getDownloadID && onCapture ? seconds => onCapture(note, seconds) : undefined} onRemove={onRemoveFrame ? frameId => onRemoveFrame(note, frameId) : undefined}/>
      {presentation.quote && (() => {
        const jump = note.textAnchor ? onJumpText : onJumpPDF
        const quote = presentation.quote
        return jump && !unresolvedAnchorIds.includes(note.id)
          ? <button className="note-source-excerpt note-excerpt-jump" type="button" title="回到原文" aria-label={`从摘录回到原文 ${quote.slice(0, 40)}`} onClick={() => jump(note.id)}>{quote}</button>
          : <blockquote className="note-source-excerpt">{quote}</blockquote>
      })()}
      {presentation.body && <NoteBody id={note.id} body={presentation.body} active={active || editing} tasksDisabled={unavailable || editing} onTaskChange={(offset, checked) => onTaskChange(note, offset + note.body.length - presentation.body.length, checked)}/>}
      {Boolean(note.tags?.length) && <div className="note-tags" aria-label="笔记标签">{note.tags!.map(value => <button key={value} type="button" aria-label={`筛选标签 ${value}`} aria-pressed={selectedTag === value} onClick={() => onTagChange(value)}>#{value}</button>)}</div>}
      <div className="note-record-footer">
        <time dateTime={new Date(note.createdAt * 1000).toISOString()}>{new Date(note.createdAt * 1000).toLocaleDateString('zh-CN')}</time>
        {captureScreen && onCaptureCurrent && <button className="icon-button" title="新增当前画面" aria-label={`新增笔记截屏 ${note.body.slice(0, 24)}`} disabled={unavailable || editing || Boolean(capturePending)} onClick={() => onCaptureCurrent(note)}><Camera size={15}/></button>}
        {getDownloadID && !note.frames?.length && !note.frame && onCaptureNote && <button className="icon-button" title="保存此处画面" aria-label={`保存笔记画面 ${note.body.slice(0, 24)}`} disabled={unavailable || editing} onClick={() => onCaptureNote(note)}><Camera size={15}/></button>}
        <div className="note-actions"><button className="icon-button" aria-label={`复制笔记 ${note.body.slice(0, 24)}`} title={copiedId === note.id ? '已复制' : '复制笔记'} onClick={() => onCopy(note)}>{copiedId === note.id ? <Check size={14}/> : <Copy size={14}/>}</button><button className="icon-button" aria-label={`编辑笔记 ${note.body.slice(0, 24)}`} title={hasDraft ? '先保存或清空当前草稿' : '编辑笔记'} disabled={unavailable || hasDraft} onClick={() => onEdit(note)}><Pencil size={15} /></button><button className="icon-button" aria-label={`删除笔记 ${note.body.slice(0, 24)}`} title="删除笔记" disabled={unavailable || editing} onClick={() => { deleteVersion.current = note.updatedAt; setDeleteId(note.id) }}><Trash2 size={15} /></button></div>
      </div>
      {deleteId === note.id && <div className="note-delete"><p>删除这条笔记？</p><button className="button button-secondary" disabled={unavailable} onClick={() => setDeleteId(null)}>保留笔记</button><button className="button button-primary" disabled={unavailable} onClick={() => { void onRemove(note, deleteVersion.current).then(ok => { setDeleteId(null); if (ok) { onNotice('笔记已删除。'); onDeleted?.() } }) }}>确认删除</button></div>}
    </li>
  })}</ol>
}

export function NoteLocation({ note, contentId, showTime, unresolved, onSeek, onJumpText, onJumpPDF, onNotice }: {
  note: ReadingNote; contentId: string; showTime: boolean; unresolved: boolean
  onSeek?: (position: number) => boolean | void; onJumpText?: (id: string) => void; onJumpPDF?: (id: string) => void; onNotice: (message: string) => void
}) {
  if (note.pdfAnchor) {
    return onJumpPDF && !unresolved
      ? <button className="note-text-jump" onClick={() => onJumpPDF(note.id)} aria-label={`跳转笔记 PDF 第 ${note.pdfAnchor.page} 页 ${note.pdfAnchor.quote.slice(0, 40)}`}><Quote size={13}/>PDF · 第 {note.pdfAnchor.page} 页</button>
      : <span>{unresolved ? 'PDF 文件或页面已变化，暂无法定位' : `PDF · 第 ${note.pdfAnchor.page} 页`}</span>
  }
  if (note.textAnchor) {
    return onJumpText && !unresolved
      ? <button className="note-text-jump" onClick={() => onJumpText(note.id)} aria-label={`跳转笔记原文 ${note.textAnchor.quote.slice(0, 40)}`}><Quote size={13}/>原文位置</button>
      : <span>{unresolved ? '原文已变化，暂无法定位' : '已关联原文'}</span>
  }
  if (note.positionSeconds === null) return <span>{showTime ? '未关联时间' : '随记'}</span>
  const position = note.positionSeconds
  const time = formatPosition(position)
  const original = noteOriginalAt(contentId, position)
  const seek = () => { if (onSeek && onSeek(position) === false) onNotice('关联时间超出当前媒体范围，暂无法跳转。') }
  if (onSeek) return <span className="note-time-actions"><button className="note-time-seek" onClick={seek} aria-label={`跳转笔记关联时间 ${time}`}><Clock3 size={13}/>{time}</button>{original && <a href={original} target="_blank" rel="noopener noreferrer" aria-label={`在原视频打开笔记时间 ${time}`}><ExternalLink size={12}/></a>}</span>
  return original
    ? <a href={original} target="_blank" rel="noopener noreferrer" aria-label={`打开笔记关联时间 ${time}`}><Clock3 size={13}/>{time}<ExternalLink size={12}/></a>
    : <span><Clock3 size={13}/>{time}</span>
}
