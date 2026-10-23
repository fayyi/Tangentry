import { Button } from '../../components/ui'
import { Link2, RotateCw, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { request } from '../../shared/api/client'

export function XiaohongshuSourceForm({ id, onUpdated, onClose }: {
  id: string; onUpdated: (item: ContentItem) => void; onClose: () => void
}) {
  const [link, setLink] = useState(''), [error, setError] = useState(''), [busy, setBusy] = useState(false)
  const pending = useRef<AbortController | null>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { input.current?.focus(); return () => pending.current?.abort() }, [id])
  async function update() {
    if (pending.current || !link.trim()) return
    const controller = new AbortController()
    pending.current = controller
    setBusy(true); setError('')
    try {
      const item = await request<ContentItem>('/xiaohongshu/details/' + encodeURIComponent(id) + '/refresh', {
        method: 'POST', body: JSON.stringify({ url: link.trim() }), signal: controller.signal,
      })
      if (!controller.signal.aborted) {
        if (item.id !== id) throw new Error('分享链接与当前笔记不一致。')
        onUpdated(item)
      }
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '笔记暂时无法读取。')
    } finally {
      if (!controller.signal.aborted) { pending.current = null; setBusy(false) }
    }
  }
  return <form className="reader-source-form" aria-label="更新小红书分享链接" onSubmit={event => { event.preventDefault(); void update() }}>
    <div className="reader-source-form-heading"><label htmlFor="xhs-source-link"><Link2 size={15}/>分享链接</label><Button variant="quiet" iconOnly type="button" className="icon-button" title="关闭链接输入" aria-label="关闭链接输入" onClick={onClose}><X size={16}/></Button></div>
    <textarea ref={input} id="xhs-source-link" value={link} disabled={busy} onChange={event => setLink(event.target.value)} rows={2} maxLength={2048} placeholder="粘贴这篇笔记的完整分享链接"/>
    <div className="reader-source-form-actions"><Button variant="secondary" className="button button-secondary" disabled={busy || !link.trim()}><RotateCw size={14}/>{busy ? '正在读取' : '读取笔记'}</Button><a className="button button-secondary" href="/tools#accounts">检查登录</a></div>
    {error && <p className="error-text" role="alert">{error}</p>}
  </form>
}
