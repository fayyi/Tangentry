import type { AIPromptTemplate } from './types'

export type PromptTask = 'summary' | 'translation' | 'chat'

export const EMPTY_TEMPLATES: Record<PromptTask, AIPromptTemplate> = {
  summary: {
    system: '', user: '',
  },
  translation: {
    system: '', user: '',
  },
  chat: {
    system: '', user: '',
  },
}

export const PROMPT_TASKS: Array<{ id: PromptTask; label: string; description: string }> = [
  { id: 'summary', label: '摘要', description: '提炼内容重点并生成结构化摘要' },
  { id: 'translation', label: '翻译', description: '将内容翻译成目标语言并保留段落' },
  { id: 'chat', label: '对话', description: '围绕当前内容回答阅读问题' },
]

export const PROMPT_VARIABLES = [
  { name: 'title', label: '标题', usage: '摘要、对话', description: '文章或视频标题', tasks: ['summary', 'chat'] as PromptTask[] },
  { name: 'source_type', label: '来源类型', usage: '摘要、对话', description: '例如文章、视频或播客', tasks: ['summary', 'chat'] as PromptTask[] },
  { name: 'content', label: '内容', usage: '摘要、翻译、对话', description: '当前任务要处理的正文', tasks: ['summary', 'translation', 'chat'] as PromptTask[] },
  { name: 'target_language', label: '目标语言', usage: '翻译', description: '翻译输出语言', tasks: ['translation'] as PromptTask[] },
] as const

export type PromptVariableName = (typeof PROMPT_VARIABLES)[number]['name']
export const PROMPT_VARIABLE_NAMES = new Set<string>(PROMPT_VARIABLES.map(variable => variable.name))

export function normalizeTemplate(value: AIPromptTemplate | undefined, task: PromptTask, defaults: Record<PromptTask, AIPromptTemplate>): AIPromptTemplate {
  const fallback = defaults[task]
  return {
    system: value?.system ?? fallback.system,
    user: value?.user ?? fallback.user,
  }
}

export function templatesEqual(a: AIPromptTemplate, b: AIPromptTemplate): boolean {
  return a.system === b.system && a.user === b.user
}

export function changedTemplateFields(template: AIPromptTemplate, task: PromptTask, defaults: Record<PromptTask, AIPromptTemplate>): string[] {
  const fallback = defaults[task]
  return [
    ...(template.system !== fallback.system ? ['系统提示'] : []),
    ...(template.user !== fallback.user ? ['用户提示'] : []),
  ]
}

export function resetTemplate<T extends Record<PromptTask, AIPromptTemplate>>(templates: T, task: PromptTask, defaults: Record<PromptTask, AIPromptTemplate>): T {
  return { ...templates, [task]: defaults[task] }
}

export function templateError(value: string): string | null {
  for (let cursor = 0; cursor < value.length;) {
    const start = value.indexOf('{{', cursor)
    if (start < 0) return null
    const end = value.indexOf('}}', start + 2)
    if (end < 0) return '变量缺少结束符 }}。'
    const name = value.slice(start + 2, end).trim()
    if (!name) return '变量名称不能为空。'
    if (!PROMPT_VARIABLE_NAMES.has(name)) return `未知变量：{{${name}}}。`
    cursor = end + 2
  }
  return null
}

export function renderTemplate(value: string, variables: Record<string, string>): { value: string; error: string | null } {
  const error = templateError(value)
  if (error) return { value: '', error }
  return {
    value: value.replace(/{{\s*([^{}]+?)\s*}}/g, (_match, name: string) => variables[name.trim()] ?? ''),
    error: null,
  }
}

export function insertVariable(value: string, variable: PromptVariableName, selectionStart?: number | null, selectionEnd?: number | null): { value: string; start: number; end: number } {
  const start = Math.max(0, Math.min(selectionStart ?? value.length, value.length))
  const end = Math.max(start, Math.min(selectionEnd ?? start, value.length))
  const token = `{{${variable}}}`
  return { value: `${value.slice(0, start)}${token}${value.slice(end)}`, start: start + token.length, end: start + token.length }
}
