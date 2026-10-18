import type { ReactNode } from 'react'

export function DiscussionFallback({ children, discussion, loading, error, onRetry }: {
  discussion?: ReactNode
  children: ReactNode
  loading: boolean
  error: string
  onRetry: () => void
}) {
  if (discussion) return <>{discussion}</>
  return <>
    {children}
    {loading && <p className="field-hint" role="status">正在加载话题楼层…</p>}
    {error && <div className="error-panel" role="alert"><p>{error}</p><button type="button" className="button button-secondary" onClick={onRetry}>重试</button></div>}
  </>
}
