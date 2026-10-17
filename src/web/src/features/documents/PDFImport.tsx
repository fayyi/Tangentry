import { Upload } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { ContentItem } from '../../shared/api/types'

export function PDFImport({ onImported }: { onImported: (item: ContentItem) => void }) {
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  async function upload(file?: File) {
    if (!file || controller.current) return
    if (file.size > 50 * 1024 * 1024) { setError('PDF 文件最大 50 MB。'); return }
    const current = new AbortController(); controller.current = current; setBusy(true); setError('')
    const body = new FormData(); body.append('file', file)
    try {
      const item = await request<ContentItem>('/documents', { method: 'POST', body, signal: current.signal })
      if (!current.signal.aborted) onImported(item)
    } catch (cause) { if (!current.signal.aborted) setError(cause instanceof Error ? cause.message : '文档未能导入。') }
    finally { controller.current = null; if (!current.signal.aborted) setBusy(false) }
  }
  return <div className="pdf-import"><label className="cookie-import"><Upload size={15} aria-hidden="true" />{busy ? '正在导入…' : '导入 PDF 到资料库'}<input aria-label="导入 PDF 文件" type="file" accept=".pdf,application/pdf" disabled={busy} onChange={event => { void upload(event.target.files?.[0]); event.target.value = '' }} /></label>{error && <p role="alert" className="field-hint">{error}</p>}</div>
}
