import { Check, LoaderCircle, Trash2, UserRound, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import type { DiscoveryFeedback } from '../../shared/api/types'

const labels: Record<string, string> = { more: '想多看看', less: '暂时少些（7 天）', hide: '不感兴趣', block_creator: '屏蔽此创作者' }
const targetLabels: Record<string, string> = { item: '这张卡片', source: '同来源', creator: '同创作者', topic: '同主题' }

export function DiscoveryFeedback() {
  const [values, setValues] = useState<DiscoveryFeedback[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  async function reload() {
    setLoading(true)
    try { const result = await request<{ feedback: DiscoveryFeedback[] }>('/discovery/feedback'); setValues(result.feedback) } catch { setNotice('反馈暂时无法读取，请重试。') } finally { setLoading(false) }
  }
  useEffect(() => { void reload() }, [])
  async function remove(id: string) {
    setBusy(id)
    try { await request(`/discovery/feedback/${encodeURIComponent(id)}`, { method: 'DELETE' }); setValues(current => current.filter(value => value.id !== id)); setNotice('已撤销这条反馈。') } catch { setNotice('撤销失败，请重试。') } finally { setBusy('') }
  }
  async function clear() {
    setBusy('all')
    try { await request('/discovery/feedback', { method: 'DELETE' }); setValues([]); setNotice('已清除全部发现反馈。') } catch { setNotice('清除失败，请重试。') } finally { setBusy('') }
  }
  return <section className="settings-section" aria-labelledby="discovery-feedback-title">
    <div className="settings-heading"><div><h2 id="discovery-feedback-title">发现偏好</h2><p className="field-hint">反馈只影响发现页的近期展示，不会根据观看记录建立长期兴趣画像。</p></div><UserRound size={19} aria-hidden="true" /></div>
    {notice && <p className="settings-feedback" role="status"><Check size={15} />{notice}</p>}
    {loading ? <p className="field-hint"><LoaderCircle size={15} className="spin" />正在读取反馈…</p> : values.length === 0 ? <p className="field-hint">还没有保存的发现反馈。</p> : <div className="discovery-feedback-list">{values.map(value => <div className="setting-row" key={value.id}><div><strong>{labels[value.type] ?? value.type}</strong><p>{targetLabels[value.target] ?? value.target}：{value.value}{value.expiresAt ? ' · 7 天内有效' : ''}</p></div><button type="button" className="icon-button" aria-label={`撤销${labels[value.type] ?? '反馈'}`} title="撤销反馈" disabled={busy === value.id} onClick={() => void remove(value.id)}>{busy === value.id ? <LoaderCircle size={15} className="spin" /> : <X size={16} />}</button></div>)}</div>}
    {values.length > 0 && <button type="button" className="text-button" disabled={busy === 'all'} onClick={() => void clear()}><Trash2 size={15} />清除所有反馈</button>}
  </section>
}
