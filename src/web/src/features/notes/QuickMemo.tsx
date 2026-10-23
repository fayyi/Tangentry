import { Button } from '../../components/ui'
import { useEffect, useRef, useState } from 'react'
import { Trash2 } from 'lucide-react'
import { APIError, request } from '../../shared/api/client'
import type { ReadingNote } from '../../shared/api/types'
import { notifyNotesChanged } from './changes'
import { MarkdownEditor } from './MarkdownEditor'
import { TagInput, parseTags, tagValidation } from './TagInput'
import { deleteRecoveryMessage, persistRecovery, readRecoveryOrigin, recoveryDrafts, recoveryKey, rememberRecoveryOrigin, removeRecovery, type RecoveryDraft } from './draftStorage'
import './notes.css'

const draftKey = 'tangentry.memo-draft.v1'
const recoveryScope = 'quick-memo'
function readDraft(raw?: string) {
  try {
    const value = JSON.parse(raw ?? sessionStorage.getItem(draftKey) ?? 'null')
    if (value && typeof value.body === 'string' && value.body.length <= 16000 && typeof value.tags === 'string' && value.tags.length <= 512) return { body: value.body as string, tags: value.tags as string, createId: typeof value.createId === 'string' && /^[a-f0-9]{32}$/.test(value.createId) ? value.createId as string : undefined }
  } catch { /* Optional session draft storage. */ }
  return { body: '', tags: '', createId: undefined as string | undefined }
}

export function QuickMemo() {
  const [draft, setDraft] = useState(() => readDraft())
  const backupKey = recoveryKey(recoveryScope)
  const [backups, setBackups] = useState(() => recoveryDrafts(recoveryScope))
  const [backupFailed, setBackupFailed] = useState(false)
  const [backupNotice, setBackupNotice] = useState('')
  const recovered = useRef<RecoveryDraft | null>(readRecoveryOrigin(recoveryScope))
  const [preview, setPreview] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [creationConflict, setCreationConflict] = useState(false)
  const [notice, setNotice] = useState('')
  const writing = useRef(false)
  const editor = useRef<HTMLTextAreaElement>(null)
  const count = Array.from(draft.body).length
  const hasDraft = Boolean(draft.body || draft.tags)
  const recoverable = backups.filter(backup => backup.key !== backupKey && backup.raw !== JSON.stringify(draft) && Boolean(readDraft(backup.raw).body || readDraft(backup.raw).tags))
  useEffect(() => {
    const refresh = () => setBackups(recoveryDrafts(recoveryScope))
    window.addEventListener('storage', refresh)
    return () => window.removeEventListener('storage', refresh)
  }, [])
  useEffect(() => {
    try {
      if (draft.body || draft.tags) sessionStorage.setItem(draftKey, JSON.stringify(draft))
      else sessionStorage.removeItem(draftKey)
    } catch { /* Disabled storage does not prevent explicit saving. */ }
    setBackupFailed(!persistRecovery(backupKey, hasDraft ? JSON.stringify(draft) : null))
  }, [draft, backupKey, hasDraft])
  function clearDraft() {
    if (recovered.current) removeRecovery(recovered.current)
    recovered.current = null
    rememberRecoveryOrigin(recoveryScope, null)
    setDraft({ body: '', tags: '', createId: undefined })
    setCreationConflict(false)
    setBackups(recoveryDrafts(recoveryScope))
    setPreview(false)
  }
  function deleteBackup(backup: RecoveryDraft) {
    setBackupNotice(deleteRecoveryMessage(backup))
    setBackups(recoveryDrafts(recoveryScope))
  }
  async function save() {
    if (writing.current) return
    setError(''); setNotice('')
    if (tagValidation(draft.tags)) { document.getElementById('quick-memo-tags')?.focus(); return }
    if (!draft.body.trim() || count > 8000) { setError('请填写 1–8000 字的笔记。'); editor.current?.focus(); return }
    writing.current = true; setPending(true)
    const prepared = { ...draft, createId: draft.createId ?? crypto.randomUUID().replaceAll('-', '') }
    setDraft(prepared)
    try { sessionStorage.setItem(draftKey, JSON.stringify(prepared)) } catch { /* Retry identity remains in memory. */ }
    setBackupFailed(!persistRecovery(backupKey, JSON.stringify(prepared)))
    try {
      const note = await request<ReadingNote>('/notes', { method: 'POST', body: JSON.stringify({ body: prepared.body, tags: parseTags(prepared.tags), createId: prepared.createId }) })
      clearDraft()
      notifyNotesChanged(note.contentId)
      setNotice('笔记已保存。')
      requestAnimationFrame(() => editor.current?.focus({ preventScroll: true }))
    } catch (cause) { setError(cause instanceof Error ? cause.message : '笔记没有保存，请重试。'); setCreationConflict(cause instanceof APIError && ['note_creation_conflict', 'note_creation_deleted'].includes(cause.code)) }
    finally { writing.current = false; setPending(false) }
  }
  return <section className="quick-memo" aria-label="随手记">
    {backupNotice && <p className="notes-hint" role="status">{backupNotice}</p>}
    {recoverable.length > 0 && <details className="note-recovery"><summary>可恢复随手记 · {recoverable.length}</summary>{recoverable.map(backup => {
      const value = readDraft(backup.raw)
      return <div key={backup.key}><p>{value.body.slice(0, 100) || value.tags}</p><time>{new Date(backup.updatedAt).toLocaleString('zh-CN')}</time><div><Button variant="secondary" type="button" className="button button-secondary" disabled={hasDraft || pending} title={hasDraft ? '先保存或清空当前草稿' : '恢复此草稿'} onClick={() => { recovered.current = backup; rememberRecoveryOrigin(recoveryScope, backup); setDraft(value); setPreview(false); setError(''); setNotice('草稿已恢复，尚未保存为笔记。') }}>恢复随手记</Button><Button variant="quiet" iconOnly type="button" className="icon-button" disabled={pending} title="删除随手记备份" aria-label={`删除随手记备份 ${value.body.slice(0, 24) || value.tags}`} onClick={() => deleteBackup(backup)}><Trash2 size={14}/></Button></div></div>
    })}</details>}
    {backupFailed && hasDraft && <p className="notes-error" role="status">无法备份草稿到此浏览器，请保存笔记后再关闭页面。</p>}
    <form className="note-editor" onSubmit={event => { event.preventDefault(); void save() }}>
      <MarkdownEditor id="quick-memo-body" label="随手记" ariaLabel="随手记正文" value={draft.body} onChange={body => { setDraft(current => ({ ...current, body })); setNotice(''); setError('') }} editorRef={editor} preview={preview} onPreviewChange={setPreview} pending={pending} rows={3} onSave={() => void save()} invalid={Boolean(error && (!draft.body.trim() || count > 8000))} describedBy="quick-memo-status" />
      <TagInput id="quick-memo-tags" value={draft.tags} onChange={tags => setDraft(current => ({ ...current, tags }))} disabled={pending} />
      <div className="quick-memo-footer"><span className="note-counter">{count} / 8000</span>{hasDraft && <Button variant="secondary" type="button" className="button button-secondary" disabled={pending} onClick={() => { clearDraft(); setError(''); setNotice('') }}>清空随手记草稿</Button>}<Button variant="primary" type="submit" className="button button-primary" disabled={pending || !draft.body.trim()}>{pending ? '正在保存' : '保存随手记'}</Button></div>
      <p id="quick-memo-status" className={error ? 'notes-error' : 'notes-hint'} role={error ? 'alert' : notice ? 'status' : undefined}>{error ? `${error} 正文草稿保留，请重试。` : notice}</p>
      {creationConflict && <Button variant="secondary" type="button" className="button button-secondary" disabled={pending} onClick={() => { setDraft(current => ({ ...current, createId: undefined })); setCreationConflict(false); setError(''); setNotice('草稿已保留，点击保存将另存一条笔记。') }}>另存草稿</Button>}
    </form>
  </section>
}
