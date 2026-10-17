import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'

interface Job { id: string; state: 'fetching' | 'converting' | 'ready' | 'failed'; message?: string }

export function useCompatibilityPlayback(identity: string, url: string) {
  const [attempt, setAttempt] = useState(0)
  const [job, setJob] = useState<Job | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { setAttempt(0); setJob(null); setError('') }, [identity, url])
  useEffect(() => {
    if (!attempt) return
    let active = true, token = '', timer: number | undefined
    const controller = new AbortController()
    setJob(null)
    setError('')
    const release = () => { if (token) void request('/playback/compatibility/' + token, { method: 'DELETE', keepalive: true }).catch(() => {}) }
    const poll = async () => {
      try {
        const value = await request<Job>('/playback/compatibility/' + token, { signal: controller.signal })
        if (!active) return
        setJob(value)
        if (value.state === 'failed') { setError(value.message || '兼容播放未完成。'); release() }
        else if (value.state !== 'ready') timer = window.setTimeout(() => void poll(), 750)
      } catch (cause) {
        if (active) { setError(cause instanceof Error ? cause.message : '兼容播放暂不可用。'); release() }
      }
    }
    void (async () => {
      try {
        // Receive the token even after leaving, so a late-created session can be released.
        const value = await request<Job>('/playback/compatibility', { method: 'POST', body: JSON.stringify({ url }) })
        token = value.id
        if (!active) { release(); return }
        setJob(value)
        await poll()
      } catch (cause) {
        if (active) setError(cause instanceof Error ? cause.message : '兼容播放暂不可用。')
      }
    })()
    const leave = () => { active = false; window.clearTimeout(timer); controller.abort(); release() }
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) { setJob(null); setAttempt(value => value > 0 ? value + 1 : value) }
    }
    window.addEventListener('pagehide', leave)
    window.addEventListener('pageshow', restore)
    return () => { leave(); window.removeEventListener('pagehide', leave); window.removeEventListener('pageshow', restore) }
  }, [attempt, identity, url])
  return {
    active: attempt > 0 || Boolean(error),
    source: job?.state === 'ready' && !error ? '/api/playback/compatibility/' + job.id + '/media' : '',
    pending: attempt > 0 && !error && job?.state !== 'ready',
    message: error || (job?.state === 'converting' ? '正在处理视频兼容…' : '正在准备兼容视频…'),
    error,
    start: () => setAttempt(value => value + 1),
    cancel: () => { setAttempt(0); setJob(null); setError('已取消兼容处理。') },
    fail: () => setError('兼容视频暂时无法播放，请重试或刷新笔记详情。'),
  }
}
