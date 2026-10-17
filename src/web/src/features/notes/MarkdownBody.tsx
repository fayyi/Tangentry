import { lazy, Suspense } from 'react'
import type { TaskControls } from './taskList'
const MarkdownRenderer = lazy(() => import('./MarkdownRenderer'))
export function MarkdownBody({ children, className = '', ...tasks }: { children: string; className?: string } & TaskControls) {
  return <Suspense fallback={<div className={className}>{children}</div>}><MarkdownRenderer className={className} {...tasks}>{children}</MarkdownRenderer></Suspense>
}
