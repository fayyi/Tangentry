import { Button } from '../../components/ui'
import { Download, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { requestResponse } from '../../shared/api/client'
import { saveFile } from '../../shared/browser/saveFile'

type ImportResult = { created: number; existing: number; skipped: number; failed: number; ungrouped?: number }

export function SubscriptionPortability() {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [error, setError] = useState(false)

  async function exportOPML() {
    setBusy(true); setFeedback(''); setError(false)
    try {
      const response = await requestResponse('/subscriptions/opml', {}, { message: '导出订阅失败', code: 'opml_export_failed' })
      saveFile(await response.blob(), 'tangentry-subscriptions.opml')
      setFeedback('已导出订阅来源')
    } catch (cause) {
      setError(true); setFeedback(cause instanceof Error ? cause.message : '导出订阅失败')
    } finally { setBusy(false) }
  }

  async function importOPML(file: File) {
    setBusy(true); setFeedback(''); setError(false)
    try {
      const raw = await file.text()
      const response = await requestResponse('/subscriptions/opml', { method: 'POST', headers: { 'Content-Type': 'application/xml' }, body: raw }, { message: '导入订阅失败', code: 'opml_import_failed' })
      const body = await response.json().catch(() => null) as ImportResult | null
      setFeedback(`已导入 ${body?.created ?? 0} 个来源；${body?.existing ?? 0} 个已存在${body?.failed ? `；${body.failed} 个失败` : ''}${body?.ungrouped ? `；${body.ungrouped} 个分组未恢复` : ''}`)
      window.dispatchEvent(new CustomEvent('tangentry:subscriptions-changed'))
    } catch (cause) {
      setError(true); setFeedback(cause instanceof Error ? cause.message : '导入订阅失败')
    } finally { setBusy(false); if (input.current) input.current.value = '' }
  }

  return <div className="subscription-portability">
    <div className="subscription-portability-copy"><strong>带走你的来源</strong><span>用 OPML 在 Tangentry 与其他阅读器之间迁移 RSS、播客和频道。</span></div>
    <div className="subscription-portability-actions">
      <Button variant="secondary" className="button button-secondary" disabled={busy} onClick={() => void exportOPML()}><Download size={15} aria-hidden="true" />导出 OPML</Button>
      <Button variant="secondary" className="button button-secondary" disabled={busy} onClick={() => input.current?.click()}><Upload size={15} aria-hidden="true" />导入 OPML</Button>
      <input ref={input} type="file" hidden accept=".opml,.xml,application/xml,text/xml" aria-label="选择 OPML 文件" onChange={event => { const file = event.target.files?.[0]; if (file) void importOPML(file) }} />
    </div>
    {feedback && <p className="subscription-portability-result" role={error ? 'alert' : 'status'}>{feedback}</p>}
  </div>
}
