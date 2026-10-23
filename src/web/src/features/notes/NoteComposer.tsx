import { Button, Checkbox } from '../../components/ui'
import { Camera, Check, Clock3, Hash, ImageOff, LoaderCircle, Maximize2, Minimize2, Quote, X } from 'lucide-react'
import type { Dispatch, RefObject, SetStateAction } from 'react'
import type { SubtitleSourceVersion } from '../../shared/api/types'
import { formatPosition } from './noteTime'
import type { Draft } from './noteDraft'
import { MarkdownEditor } from './MarkdownEditor'
import { SubtitleReferences } from './SubtitleReferences'
import { TagInput } from './TagInput'

type DraftSetter = Dispatch<SetStateAction<Draft>>

export interface NoteComposerProps {
  open: boolean
  draft: Draft
  setDraft: DraftSetter
  contentId: string
  sourceVersion: SubtitleSourceVersion | null
  pending: boolean
  unavailable: boolean
  excerpt: string
  saveBody: string
  saveCount: number
  count: number
  backupFailed: boolean
  validationError: string
  preview: boolean
  onPreviewChange: (value: boolean) => void
  editor: RefObject<HTMLTextAreaElement | null>
  onChangeBody: (body: string) => void
  onSave: () => void
  showTime: boolean
  playbackPosition: number | null
  onAttachCurrentTime: () => void
  playbackHint?: string
  hasDownload: boolean
  captureScreen: boolean
  capturing: boolean
  onScreenshot: () => void
  expandedWriting: boolean
  onToggleExpanded: () => void
  tagsOpen: boolean
  onOpenTags: () => void
  onDiscardDraft: () => void
  onDetachExcerpt: () => void
  onRemoveTime: () => void
  onSaveAsNew: () => void
}

export function NoteComposer({ open, draft, setDraft, contentId, sourceVersion, pending, unavailable, excerpt, saveBody, saveCount, count, backupFailed, validationError, preview, onPreviewChange, editor, onChangeBody, onSave, showTime, playbackPosition, onAttachCurrentTime, playbackHint, hasDownload, captureScreen, capturing, onScreenshot, expandedWriting, onToggleExpanded, tagsOpen, onOpenTags, onDiscardDraft, onDetachExcerpt, onRemoveTime, onSaveAsNew }: NoteComposerProps) {
  const hasDraft = Boolean(draft.body || draft.tags || draft.positionSeconds !== null || draft.editingId || draft.textAnchor || draft.pdfAnchor || draft.subtitleReferences?.length || draft.images?.length)
  return <form id="note-composer" hidden={!open} onSubmit={event => { event.preventDefault(); onSave() }} className="note-editor" data-writing={hasDraft || preview} aria-label={draft.editingId ? '编辑来源笔记' : '新建来源笔记'}>
    {draft.pdfAnchor && <div className="note-text-anchor"><span><Quote size={13} aria-hidden="true"/>PDF · 第 {draft.pdfAnchor.page} 页</span><blockquote>{draft.pdfAnchor.quote}</blockquote><Button variant="quiet" type="button" className="note-anchor-detach" aria-label="移除笔记 PDF 关联" title="移除 PDF 关联" disabled={pending} onClick={onDetachExcerpt}><X size={13}/></Button></div>}
    {draft.textAnchor && <div className="note-text-anchor"><span><Quote size={13} aria-hidden="true"/>关联原文</span><blockquote>{draft.textAnchor.quote}</blockquote><Button variant="quiet" type="button" className="note-anchor-detach" aria-label="移除笔记原文关联" title="移除原文关联" disabled={pending} onClick={onDetachExcerpt}><X size={13}/></Button></div>}
    <SubtitleReferences references={draft.subtitleReferences} contentId={contentId} sourceVersion={sourceVersion} disabled={pending} onRemove={index => setDraft(current => ({ ...current, subtitleReferences: current.subtitleReferences?.filter((_, at) => at !== index) }))}/>
    {((draft.images?.length ?? 0) > 0) && <div className="note-draft-frame-grid">{draft.images!.map((image, index) => <figure className="note-draft-frame" key={`${image.positionSeconds}-${index}`}><img src={`data:image/jpeg;base64,${image.data}`} width={image.width} height={image.height} alt={index === 0 ? `草稿截屏 ${formatPosition(image.positionSeconds)}` : `草稿截屏 ${index + 1} · ${formatPosition(image.positionSeconds)}`}/><figcaption><Camera size={13} aria-hidden="true"/><span>截屏 {index + 1} · {formatPosition(image.positionSeconds)}</span><Button variant="quiet" iconOnly type="button" className="icon-button" title="移除截屏" aria-label={index === 0 ? '移除草稿截屏' : `移除草稿截屏 ${index + 1}`} disabled={pending} onClick={() => setDraft(current => ({ ...current, images: current.images?.filter((_, at) => at !== index) }))}><ImageOff size={14}/></Button></figcaption></figure>)}</div>}
    <MarkdownEditor compact id="note-body" label={draft.editingId ? '编辑笔记' : excerpt ? '我的批注' : '写想法'} placeholder={excerpt ? '补充想法（可选）…' : '有什么值得记下来？'} value={draft.body} onChange={onChangeBody} editorRef={editor} preview={preview} onPreviewChange={onPreviewChange} pending={pending} invalid={Boolean(validationError && (!saveBody.trim() || saveCount > 8000))} describedBy="note-validation" rows={3} onSave={() => { if (!unavailable) onSave() }} />
    {(showTime || hasDraft) && <div className="note-draft-meta">{showTime && <span className="note-auto-time"><Button variant="quiet" className="note-capture-time" type="button" title="关联当前播放时间" aria-label="关联当前播放时间" disabled={pending || playbackPosition === null} onClick={onAttachCurrentTime}><Clock3 size={13} aria-hidden="true"/><output aria-label="笔记关联时间">{draft.positionSeconds === null ? '关联时间' : formatPosition(draft.positionSeconds)}</output></Button>{draft.positionSeconds !== null && <Button variant="quiet" type="button" aria-label="移除笔记关联时间" disabled={pending} onClick={onRemoveTime}><X size={12}/></Button>}</span>}{hasDraft && !backupFailed && <span className="note-draft-status"><Check size={12} aria-hidden="true"/>草稿已备份</span>}{count >= 7200 && <span className="note-counter">{count} / 8000</span>}</div>}
    {playbackHint && draft.positionSeconds === null && <p className="notes-hint note-clock-hint">{playbackHint}</p>}
    {hasDownload && !(draft.images?.length) && !draft.editingId && <label className="note-save-frame"><Checkbox type="checkbox" checked={draft.captureFrame === true} disabled={pending} onChange={event => setDraft(current => ({ ...current, captureFrame: event.target.checked, frameSpec: undefined }))}/>保存画面</label>}
    <div className="note-editor-actions">
      {captureScreen && <Button variant="quiet" iconOnly type="button" className="icon-button" title="新增当前画面（⌥S）" aria-label="截屏当前画面并加入草稿" disabled={pending || capturing} onClick={onScreenshot}>{capturing ? <LoaderCircle size={14} className="spin"/> : <Camera size={14}/>}</Button>}
      <Button variant="quiet" iconOnly type="button" className="icon-button" aria-label={expandedWriting ? '收起长文写作' : '展开长文写作'} title={expandedWriting ? '收起长文写作' : '展开长文写作'} aria-expanded={expandedWriting} aria-controls="note-composer" onClick={onToggleExpanded}>{expandedWriting ? <Minimize2 size={14}/> : <Maximize2 size={14}/>}</Button>
      {hasDraft || tagsOpen ? <TagInput id="note-tags" value={draft.tags ?? ''} onChange={tags => setDraft(current => ({ ...current, tags }))} disabled={pending} /> : <Button variant="quiet" iconOnly className="icon-button" type="button" title="添加标签" aria-label="添加笔记标签" onClick={onOpenTags}><Hash size={15}/></Button>}
      <Button variant="primary" className="button button-primary" type="submit" disabled={unavailable || !saveBody.trim()}><Check size={14} aria-hidden="true"/>{pending ? '正在保存' : draft.editingId ? '保存修改' : excerpt && !draft.body.trim() ? '保存摘录' : '保存笔记'}</Button>
      {hasDraft && <Button variant="quiet" iconOnly className="icon-button note-draft-clear" type="button" title={draft.editingId ? '取消编辑' : '清空草稿'} aria-label={draft.editingId ? '取消编辑' : '清空草稿'} disabled={pending} onClick={onDiscardDraft}><X size={15}/></Button>}
      {draft.editingId && <Button variant="secondary" className="button button-secondary" type="button" disabled={pending} onClick={onSaveAsNew}>另存为新笔记</Button>}
    </div>
    <p id="note-validation" className={validationError ? 'notes-error' : 'notes-hint'} role={validationError ? 'alert' : undefined}>{validationError}</p>
  </form>
}
