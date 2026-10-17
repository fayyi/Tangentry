import { Component, type ReactNode, type RefObject } from 'react'
import { originalRunAt, originalRuns, readingRun } from './articleViewport'

interface Props {
  root: RefObject<HTMLElement | null>
  contentId: string
  text: string
  layout: unknown
  focusToken?: number
  beforeReflow: () => void
  children: ReactNode
}
type Snapshot = ReturnType<typeof readingRun>

// React's pre-mutation snapshot preserves the old geometry before translated DOM changes.
export class ArticleReflowGuard extends Component<Props> {
  getSnapshotBeforeUpdate(previous: Props): Snapshot {
    if (previous.contentId !== this.props.contentId || previous.text !== this.props.text ||
      previous.focusToken !== this.props.focusToken || previous.layout === this.props.layout) return null
    const root = this.props.root.current
    const snapshot = root ? readingRun(originalRuns(root)) : null
    this.props.beforeReflow()
    return snapshot
  }

  componentDidUpdate(_previous: Props, _state: unknown, snapshot: Snapshot) {
    const root = this.props.root.current
    if (!snapshot || !root) return
    const node = originalRunAt(root, snapshot.offset)
    if (!node) return
    const delta = node.getBoundingClientRect().top - snapshot.top
    if (Math.abs(delta) > 0.5) window.scrollBy({ top: delta, behavior: 'instant' })
  }

  render() {
    return this.props.children
  }
}
