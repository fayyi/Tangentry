import { Component, Suspense, type ReactNode } from 'react'

export class PageLoadBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() { return { failed: true } }

  render() {
    if (this.state.failed) return <div className="error-panel" role="alert"><p>页面未能加载，请重新加载后重试。</p><button className="button button-secondary" onClick={() => window.location.reload()}>重新加载页面</button></div>
    return <Suspense fallback={<p className="inline-feedback" role="status">正在打开页面…</p>}>{this.props.children}</Suspense>
  }
}
