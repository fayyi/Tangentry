import { Button } from '../../components/ui'
import type { ReactNode } from 'react'

export function DiscussionFallback({ children, discussion, loading, error, onRetry, onVerify }: {
  discussion?: ReactNode
  children: ReactNode
  loading: boolean
  error: string
  onRetry: () => void
  onVerify?: () => void
}) {
  if (discussion) return <>{discussion}</>
  return <>
    {children}
    {loading && <p className="field-hint" role="status">正在加载话题楼层…</p>}
    {error && <div className="error-panel" role="alert"><p>{error}</p><Button variant="secondary" type="button" className="button button-secondary" onClick={onRetry}>重试</Button>{onVerify && <Button variant="secondary" type="button" className="button button-secondary" onClick={onVerify}>重新验证</Button>}</div>}
  </>
}
