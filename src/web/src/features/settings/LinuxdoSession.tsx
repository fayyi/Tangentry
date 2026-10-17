import { useEffect, useRef, useState } from 'react'
import { ExternalLink, LoaderCircle } from 'lucide-react'
import { request } from '../../shared/api/client'

export function LinuxdoSession({ configured, updatedAt }: { configured: boolean; updatedAt: number }) {
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const active = useRef<AbortController | null>(null)
  useEffect(() => {
    active.current?.abort(); active.current = null
    setNotice(''); setError(''); setPending(false)
    return () => active.current?.abort()
  }, [configured, updatedAt])
  async function check() {
    if (active.current) return
    const controller = new AbortController(); active.current = controller
    setPending(true); setNotice(''); setError('')
    try {
      const result = await request<{ loggedIn: boolean; username?: string }>('/settings/linuxdo/session', { method: 'POST', signal: controller.signal })
      if (!controller.signal.aborted) setNotice(result.loggedIn ? `已登录：${result.username}` : '登录已失效，请重新授权或导入。')
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '暂时无法检查登录。')
    } finally {
      if (active.current === controller) active.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  return <div className="linuxdo-session">
    <a className="text-button" href="https://linux.do/login" target="_blank" rel="noopener noreferrer"><ExternalLink size={14} aria-hidden="true"/>原站登录</a>
    {configured && <button type="button" className="text-button" disabled={pending} onClick={() => void check()}>{pending && <LoaderCircle size={14} className="spin" aria-hidden="true"/>}{pending ? '正在检查' : '检查登录'}</button>}
    {notice && <span className="field-hint" role="status">{notice}</span>}
    {error && <span className="field-hint" role="alert">{error}</span>}
  </div>
}
