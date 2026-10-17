import type { Element, Root, RootContent } from 'hast'

function ownText(node: RootContent): string {
  if (node.type === 'text') return node.value
  if (node.type !== 'element' || ['ul', 'ol', 'input'].includes(node.tagName)) return ''
  return node.children.map(ownText).join('')
}

// Only the Markdown parser's actual task nodes receive writable source positions.
export function taskSourcePositions() {
  return (tree: Root, file: { value: unknown }) => {
    const source = String(file.value)
    function visit(node: Root | RootContent) {
      if (node.type !== 'root' && node.type !== 'element') return
      if (node.type === 'element' && node.tagName === 'li' && Array.isArray(node.properties.className) && node.properties.className.includes('task-list-item')) {
        const start = node.position?.start.offset
        const marker = start === undefined ? null : source.slice(start).match(/^(?:[-+*]|\d{1,9}[.)])[ \t]*(?:\r?\n[ \t>]*)?\[([ xX])\](?=\s|$)/)
        const first = node.children.find(child => child.type === 'element')
        const input = first?.type === 'element' && first.tagName === 'p'
          ? first.children.find(child => child.type === 'element' && child.tagName === 'input') as Element | undefined
          : first?.type === 'element' && first.tagName === 'input' ? first : undefined
        if (marker && input?.properties.type === 'checkbox' && start !== undefined) {
          input.properties['data-task-offset'] = start + marker[0].lastIndexOf('[') + 1
          input.properties.ariaLabel = `待办：${node.children.map(ownText).join('').trim() || '未命名事项'}`
        }
      }
      for (const child of node.children) visit(child)
    }
    visit(tree)
  }
}

export function changeTaskState(body: string, offset: number, checked: boolean): string | null {
  if (!Number.isInteger(offset) || offset < 1 || body[offset - 1] !== '[' || body[offset + 1] !== ']' || !/^[ xX]$/.test(body[offset] ?? '')) return null
  const mark = checked ? 'x' : ' '
  return body.slice(0, offset) + mark + body.slice(offset + 1)
}

export interface TaskControls {
  onTaskChange?: (offset: number, checked: boolean) => void
  tasksDisabled?: boolean
}
