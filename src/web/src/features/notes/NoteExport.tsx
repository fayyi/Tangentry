import { Button } from '../../components/ui'
import { Download, LoaderCircle, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { APIError, requestResponse } from '../../shared/api/client'
import { saveFile } from '../../shared/browser/saveFile'

interface Selection { q?: string; tag?: string; from?: number; to?: number; contentId?: string; pinnedOnly?: boolean }

async function downloadNotes(selection: Selection, signal: AbortSignal): Promise<number> {
  const response = await requestResponse('/notes/export', { method: 'POST', body: JSON.stringify(selection), signal }, { message: '笔记导出未能完成，请重试。', code: 'note_export_failed' })
  const count = Number(response.headers.get('X-Tangentry-Notes-Count'))
  if (!response.headers.get('Content-Type')?.startsWith('application/zip') || !Number.isInteger(count) || count < 1 || count > 10_000) throw new APIError('收到的导出文件不完整，请重新导出。', 'invalid_note_export', 502)
  const blob = await response.blob()
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  const filename = /Tangentry-notes-\d{8}-\d{6}\.zip/.exec(response.headers.get('Content-Disposition') ?? '')?.[0]
  if (!filename || !blob.size) throw new APIError('收到的导出文件不完整，请重新导出。', 'invalid_note_export', 502)
  saveFile(blob, filename)
  return count
}

export function NoteExport({ selection, label, disabled = false, compact = false }: { selection: Selection; label: string; disabled?: boolean; compact?: boolean }) {
  const [pending, setPending] = useState(false), [feedback, setFeedback] = useState(''), [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  const description = useId()
  const key = JSON.stringify([selection.q ?? '', selection.tag ?? '', selection.from ?? 0, selection.to ?? 0, selection.contentId ?? '', Boolean(selection.pinnedOnly)])
  useEffect(() => {
    setPending(false); setFeedback(''); setError('')
    return () => { controller.current?.abort(); controller.current = null }
  }, [key])
  const run = async () => {
    if (pending || disabled) return
    const next = new AbortController(); controller.current = next
    setPending(true); setFeedback(''); setError('')
    try {
      const count = await downloadNotes(selection, next.signal)
      if (!next.signal.aborted) setFeedback(`已生成 ${count} 条笔记，请在浏览器下载中查看。`)
    } catch (cause) {
      if (!next.signal.aborted) setError(cause instanceof Error ? cause.message : '笔记导出未能完成，请重试。')
    } finally { if (!next.signal.aborted) { setPending(false); controller.current = null } }
  }
  const cancel = () => { controller.current?.abort(); controller.current = null; setPending(false); setError(''); setFeedback('已取消导出，原笔记和草稿保留。') }
  return <div className="note-export-controls" data-compact={compact || undefined}>
    <div className="note-export-actions">
      <Button variant={compact ? 'quiet' : 'secondary'} iconOnly={compact} type="button" className={compact ? 'icon-button' : 'button button-secondary'} disabled={disabled || pending} aria-label={label} title={label} aria-describedby={description} onClick={() => void run()}>{pending ? <LoaderCircle size={16} className="spin" aria-hidden="true"/> : <Download size={16} aria-hidden="true"/>}{!compact && label}</Button>
      {pending && <Button variant="secondary" type="button" className="button button-secondary" onClick={cancel}><X size={14} aria-hidden="true"/>取消导出</Button>}
      <span id={description}>Markdown · 全部匹配的已保存笔记</span>
    </div>
    {pending && <p role="status">正在准备笔记导出…</p>}
    {feedback && <p role="status">{feedback}</p>}
    {error && <p role="alert">{error}</p>}
  </div>
}
