import { Button } from '../../components/ui'
import { CheckCircle2, CircleAlert, ExternalLink, KeyRound, LoaderCircle, RefreshCw, Settings2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SubscriptionSource } from '../../shared/api/types'
import type { useSubscriptions } from './useSubscriptions'
import { sourceLabel } from './sourcePresentation'

type CredentialsState = { platform: string; configured: boolean; updatedAt: number; expiresAt?: number }
type HealthState = ReturnType<typeof useSubscriptions>

const labels: Record<SubscriptionSource['refresh']['status'], string> = {
  healthy: '正常', never_checked: '尚未检查', stale: '长期无更新', auth_required: '需要验证', rate_limited: '请求受限', not_found: '来源不存在', upstream_error: '上游异常', paused: '已暂停',
}
const statusTone = (status: SubscriptionSource['refresh']['status']) => status === 'healthy' ? 'healthy' : status === 'never_checked' ? 'neutral' : status === 'paused' ? 'paused' : 'attention'
const time = (seconds: number) => seconds ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(seconds * 1000)) : '尚未记录'
const relative = (seconds: number) => seconds ? time(seconds) : '尚未检查'

function CredentialHealth() {
  const [states, setStates] = useState<CredentialsState[] | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    void request<{ platforms: CredentialsState[] }>('/settings/cookies', { signal: controller.signal }).then(value => setStates(value.platforms)).catch(() => setStates(null))
    return () => controller.abort()
  }, [])
  const now = Math.floor(Date.now() / 1000)
  return <section className="subscription-health-credentials" aria-labelledby="subscription-credentials-title">
    <div className="subscription-health-section-heading"><div><h3 id="subscription-credentials-title"><KeyRound size={16} aria-hidden="true"/>平台登录状态</h3><p>B站和 YouTube 公开频道可免登录订阅；登录只用于需要账号的额外能力。</p></div><a className="text-button" href="/tools#accounts"><Settings2 size={14} aria-hidden="true"/>去设置</a></div>
    {!states && <p className="field-hint">正在读取登录状态…</p>}
    {states && <div className="subscription-credential-grid">{states.map(state => {
      const expired = Boolean(state.expiresAt && state.expiresAt <= now)
      const label = !state.configured ? '未连接' : expired ? '已过期' : '已连接'
      return <div className="subscription-credential" key={state.platform}><span className={`subscription-health-dot ${expired || !state.configured ? 'attention' : 'healthy'}`} aria-hidden="true"/><strong>{state.platform === 'bilibili' ? 'B站' : state.platform === 'xiaoyuzhou' ? '小宇宙' : state.platform === 'xiaohongshu' ? '小红书' : state.platform === 'linuxdo' ? 'Linux.do' : state.platform === 'douyin' ? '抖音' : 'YouTube'}</strong><span>{label}</span>{expired || !state.configured ? <a className="text-button" href="/tools#accounts">重新验证</a> : <small>{state.updatedAt ? `更新于 ${time(state.updatedAt)}` : '状态已保存'}</small>}</div>
    })}</div>}
  </section>
}

export function SubscriptionHealthCenter({ state, onOpenSource }: { state: HealthState; onOpenSource: (id: string) => void }) {
  const [updating, setUpdating] = useState('')
  const sources = useMemo(() => [...state.sources].sort((a, b) => {
    const order = (status: SubscriptionSource['refresh']['status']) => status === 'healthy' ? 3 : status === 'never_checked' ? 2 : status === 'paused' ? 1 : 0
    return order(a.refresh.status) - order(b.refresh.status) || b.refresh.failures - a.refresh.failures || a.name.localeCompare(b.name, 'zh-CN')
  }), [state.sources])
  async function update(id: string) {
    setUpdating(id)
    try { await state.update(id) } finally { setUpdating('') }
  }
  return <div className="subscription-health-center">
    <div className="subscription-health-intro"><div><h2>来源健康</h2><p>把“暂时没有新内容”和“来源无法同步”分开显示。失败时会保留上次成功内容。</p></div><span className="subscription-health-summary"><CheckCircle2 size={16} aria-hidden="true"/>{sources.filter(source => source.refresh.status === 'healthy').length} 个来源正常</span></div>
    <CredentialHealth />
    <section className="subscription-health-list" aria-labelledby="subscription-health-list-title"><div className="subscription-health-section-heading"><div><h3 id="subscription-health-list-title"><CircleAlert size={16} aria-hidden="true"/>订阅来源</h3><p>最近成功、失败次数、下次检查和内容 revision 都来自本地刷新记录。</p></div></div>{sources.length === 0 ? <div className="subscription-empty"><h3>还没有订阅来源</h3><p>添加来源后，这里会持续显示同步健康状态。</p></div> : <div className="subscription-health-rows">{sources.map(source => {
      const refresh = source.refresh
      const due = refresh.nextCheckAt ? time(refresh.nextCheckAt) : refresh.enabled ? '等待安排' : '手动更新'
      return <article className="subscription-health-row" key={source.id}><div className="subscription-health-row-main"><span className={`subscription-health-dot ${statusTone(refresh.status)}`} aria-hidden="true"/><div><h4>{source.name}</h4><p>{sourceLabel(source)} · revision {refresh.contentRevision}</p></div><span className={`subscription-health-status ${statusTone(refresh.status)}`}>{labels[refresh.status]}</span></div><dl className="subscription-health-metrics"><div><dt>最近成功</dt><dd>{relative(refresh.lastCheckedAt)}</dd></div><div><dt>最近失败</dt><dd>{refresh.lastFailureAt ? time(refresh.lastFailureAt) : '无'}</dd></div><div><dt>连续失败</dt><dd>{refresh.failures}</dd></div><div><dt>下次检查</dt><dd>{due}</dd></div></dl>{refresh.message && <p className="subscription-health-message">{refresh.message}</p>}<p className="subscription-health-action">建议：{refresh.suggestedAction || '检查来源状态。'}</p><div className="subscription-health-row-actions"><Button variant="secondary" className="button button-secondary" disabled={Boolean(updating) || source.active} onClick={() => void update(source.id)}>{updating === source.id ? <LoaderCircle size={14} className="spin" aria-hidden="true"/> : <RefreshCw size={14} aria-hidden="true"/>}立即检查</Button><Button variant="quiet" className="text-button" onClick={() => onOpenSource(source.id)}>打开来源</Button><a className="text-button" href={source.url} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true"/>打开网站</a>{(refresh.status === 'auth_required' || refresh.status === 'rate_limited') && <a className="text-button" href="/tools#accounts"><KeyRound size={14} aria-hidden="true"/>去设置</a>}</div></article>
    })}</div>}</section>
  </div>
}
