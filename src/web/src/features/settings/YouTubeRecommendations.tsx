import { ArrowUpRight, LoaderCircle, RefreshCw, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { Feed, ToolReport } from '../../shared/api/types'

export function YouTubeRecommendations({ tools, onFeed, onDiscover }: {
  tools: ToolReport | null
  onFeed: (feed: Feed) => void
  onDiscover: () => void
}) {
  const runtime = tools?.youtubeRecommendation
  const [pending, setPending] = useState(false), [result, setResult] = useState(''), [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  function cancel() {
    controller.current?.abort()
    controller.current = null
    setPending(false)
  }
  async function check() {
    if (controller.current) return
    const active = new AbortController()
    controller.current = active
    setPending(true); setError(''); setResult('')
    try {
      const feed = await request<Feed>('/recommendations/youtube', { signal: active.signal })
      if (active.signal.aborted) return
      setResult(`已读取 ${feed.items.length} 条${feed.sourceTitle === 'YouTube 账号订阅动态' ? '账号订阅动态' : '首页推荐'}。`)
      onFeed(feed)
    } catch (cause) {
      if (!active.signal.aborted) setError(cause instanceof Error ? cause.message : '推荐暂不可用。')
    } finally {
      if (controller.current === active) {
        controller.current = null
        setPending(false)
      }
    }
  }
  return <section className="settings-section" aria-labelledby="youtube-discovery-title">
    <div className="settings-heading"><div><h2 id="youtube-discovery-title">推荐检测</h2><p className="field-hint">使用已保存的 YouTube 账号读取推荐。</p></div><span className="settings-state" data-ready={runtime?.available}>{runtime?.available ? '可用' : '未安装'}</span></div>
    <div className="recommendation-check-actions">
      <button aria-label="检测推荐连接" className="button button-secondary" disabled={pending || !runtime?.available} onClick={() => void check()}>{pending ? <LoaderCircle size={14} className="spin"/> : <RefreshCw size={14}/>}测试推荐</button>
      {pending && <button className="icon-button" aria-label="取消检测" title="取消检测" onClick={cancel}><X size={16}/></button>}
      {runtime?.version && <span className="field-hint">{runtime.version}</span>}
    </div>
    {result && <p className="settings-feedback" role="status">{result}</p>}
    {result && <button className="text-button" onClick={onDiscover}>查看内容<ArrowUpRight size={14} aria-hidden="true"/></button>}
    {error && <p className="settings-feedback" role="alert">{error}</p>}
  </section>
}
