import { Camera, Trash2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { NoteFrame as NoteFrameData, ReadingNote } from '../../shared/api/types'
import { formatPosition } from './NotesPanel'
import './note-frame.css'

export function NoteFrame({ note, onSeek, onCapture, onRemove, disabled = false }: {
  note: ReadingNote; onSeek?: (seconds: number) => void
  onCapture?: (seconds: number) => void; onRemove?: (frameId: string) => void; disabled?: boolean
}) {
  const frames = note.frames?.length ? note.frames : note.frame ? [note.frame] : []
  if (frames.length === 0) return null
  return <div className="note-frame-gallery" aria-label={`笔记截屏 ${frames.length} 张`}>{frames.map(frame => <NoteFrameCard key={frame.id} note={note} frame={frame} onSeek={onSeek} onCapture={onCapture} onRemove={onRemove} disabled={disabled}/>)}</div>
}

function NoteFrameCard({ note, frame, onSeek, onCapture, onRemove, disabled }: {
  note: ReadingNote; frame: NoteFrameData; onSeek?: (seconds: number) => void
  onCapture?: (seconds: number) => void; onRemove?: (frameId: string) => void; disabled: boolean
}) {
  const [time, setTime] = useState(frame?.positionSeconds ?? 0)
  const [failed, setFailed] = useState(false)
  const preview = useRef<HTMLDialogElement>(null)
  useEffect(() => { setTime(frame?.positionSeconds ?? 0); setFailed(false) }, [frame?.id, frame?.positionSeconds])
  const url = `/api/notes/${note.id}/frames/${frame.id}`
  const label = frame.source === 'player' ? `截屏 ${formatPosition(frame.positionSeconds)}` : `约 ${formatPosition(frame.positionSeconds)}`
  return <figure className="note-frame">
    {failed ? <p className="notes-error" role="status">画面暂时无法读取，笔记仍保留。</p> : <button className="note-frame-preview" type="button" title="放大画面" aria-label={`放大笔记画面 ${label}`} onClick={() => preview.current?.showModal()}>
      <img src={url} width={frame.width} height={frame.height} alt={`视频画面，${label}`} loading="lazy" onError={() => setFailed(true)}/>
    </button>}
    <figcaption>
      {onSeek ? <button type="button" className="note-frame-time" onClick={() => onSeek(frame.positionSeconds)}>{label}</button> : <span>{label}</span>}
      {onCapture && frame.source !== 'player' && <details><summary>新增画面</summary><div className="note-frame-controls"><label>画面时间<input type="number" aria-label="画面时间（秒）" min={0} max={604800} step={0.1} value={Number.isFinite(time) ? time : ''} disabled={disabled} onChange={event => setTime(event.target.valueAsNumber)}/></label><button className="icon-button" title="截取新画面" aria-label="截取新画面" disabled={disabled || !Number.isFinite(time) || time < 0 || time > 604800} onClick={() => onCapture(time)}><Camera size={16}/></button></div></details>}
      {onRemove && <button className="icon-button" title={`移除画面 ${label}`} aria-label="移除笔记画面" disabled={disabled} onClick={() => onRemove(frame.id)}><Trash2 size={15}/></button>}
    </figcaption>
    <dialog ref={preview} className="note-frame-dialog" aria-label="笔记画面预览" onClick={event => { if (event.target === event.currentTarget) preview.current?.close() }}>
      <button className="icon-button" title="关闭画面预览" aria-label="关闭画面预览" onClick={() => preview.current?.close()}><X size={20}/></button>
      <img src={url} width={frame.width} height={frame.height} alt={`视频画面，${label}`}/>
      <p>{label}</p>
    </dialog>
  </figure>
}
