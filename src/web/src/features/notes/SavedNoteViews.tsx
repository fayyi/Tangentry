import { Bookmark, Check, Pencil, Plus, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { hasSelection, selectionKey, useSavedNoteViews } from './savedViews'
import type { NoteViewSelection } from './savedViews'

export function SavedNoteViews({ selection, canSave, onApply }: {
  selection: NoteViewSelection; canSave: boolean; onApply: (selection: NoteViewSelection) => void
}) {
  const saved = useSavedNoteViews()
  const [editor, setEditor] = useState<{ id?: string; selection: NoteViewSelection; name: string } | null>(null)
  const addButton = useRef<HTMLButtonElement>(null)
  const activeButton = useRef<HTMLButtonElement>(null)
  const active = saved.views.find(view => selectionKey(view) === selectionKey(selection))
  const close = () => { setEditor(null); requestAnimationFrame(() => (addButton.current?.disabled ? activeButton.current : addButton.current)?.focus({ preventScroll: true })) }
  const open = (id?: string) => {
    const view = saved.views.find(item => item.id === id)
    setEditor({ id, selection: view ?? selection, name: view?.name ?? Array.from([selection.pinnedOnly && '固定笔记', selection.tag && `#${selection.tag}`, selection.query, (selection.fromDate || selection.toDate) && '日期筛选'].filter(Boolean).join(' · ')).slice(0, 64).join('') })
  }
  if (!saved.views.length && !hasSelection(selection) && !saved.error) return null
  return <div className="saved-note-views">
    <div className="note-view-toolbar">
      {saved.views.length > 0 && <nav className="note-view-list" aria-label="笔记筛选视图">
        {saved.views.map(view => <button ref={active?.id === view.id ? activeButton : undefined} key={view.id} type="button" className="note-view-button" aria-pressed={active?.id === view.id} title={[view.name, view.query, view.tag && `#${view.tag}`, view.fromDate, view.toDate].filter(Boolean).join(' · ')} onClick={() => { setEditor(null); onApply(view) }}><Bookmark size={14}/><span>{view.name}</span></button>)}
      </nav>}
      {active && <><button type="button" className="icon-button" title="重命名当前视图" aria-label={`重命名视图：${active.name}`} onClick={() => open(active.id)}><Pencil size={15}/></button><button type="button" className="icon-button" title="删除当前视图" aria-label={`删除视图：${active.name}`} onClick={() => { if (saved.remove(active.id)) close() }}><X size={15}/></button></>}
      <button ref={addButton} type="button" className={saved.views.length ? 'icon-button' : 'button button-secondary note-view-save'} aria-label="保存当前笔记筛选" title={active ? '这组筛选已保存' : '保存当前笔记筛选'} disabled={!canSave || !hasSelection(selection) || Boolean(active)} onClick={() => open()}><Plus size={16}/>{saved.views.length === 0 && '保存筛选'}</button>
    </div>
    {editor && <form className="note-view-editor" aria-label={editor.id ? '重命名笔记视图' : '保存笔记视图'} onSubmit={event => {
      event.preventDefault()
      if (editor.id ? saved.rename(editor.id, editor.name) : saved.save(editor.name, editor.selection)) close()
    }} onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); close() } }}>
      <input autoFocus aria-label="笔记视图名称" placeholder="视图名称" maxLength={64} value={editor.name} onChange={event => setEditor({ ...editor, name: event.target.value })}/>
      <button type="submit" className="icon-button" title="保存视图" aria-label="确认保存笔记视图" disabled={!editor.name.trim()}><Check size={16}/></button>
      <button type="button" className="icon-button" title="取消" aria-label="取消编辑笔记视图" onClick={close}><X size={16}/></button>
    </form>}
    {saved.error && <p className="notes-hint" role="alert">{saved.error}</p>}
  </div>
}
