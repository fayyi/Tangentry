import { describe, expect, it } from 'vitest'
import { DEFAULT_TEMPLATES, changedTemplateFields, insertVariable, renderTemplate, resetTemplate, templateError, templatesEqual } from './promptTemplates'

describe('prompt template helpers', () => {
  it('compares defaults and reports changed fields', () => {
    expect(templatesEqual(DEFAULT_TEMPLATES.summary, DEFAULT_TEMPLATES.summary)).toBe(true)
    expect(changedTemplateFields({ ...DEFAULT_TEMPLATES.summary, user: '自定义 {{content}}' }, 'summary')).toEqual(['用户提示'])
  })

  it('resets only the selected task', () => {
    const edited = { ...DEFAULT_TEMPLATES, summary: { ...DEFAULT_TEMPLATES.summary, user: '自定义摘要' }, chat: { ...DEFAULT_TEMPLATES.chat, system: '自定义对话' } }
    const reset = resetTemplate(edited, 'summary')
    expect(reset.summary).toEqual(DEFAULT_TEMPLATES.summary)
    expect(reset.chat).toEqual(edited.chat)
  })

  it('inserts a variable over a selection and preserves the caret', () => {
    expect(insertVariable('标题：旧内容', 'content', 3, 6)).toEqual({ value: '标题：{{content}}', start: 14, end: 14 })
    expect(insertVariable('abc', 'title')).toEqual({ value: 'abc{{title}}', start: 12, end: 12 })
  })

  it('renders local examples and rejects malformed variables', () => {
    expect(renderTemplate('{{title}} / {{content}}', { title: '示例标题', content: '脱敏内容' })).toEqual({ value: '示例标题 / 脱敏内容', error: null })
    expect(templateError('{{missing}}')).toBe('未知变量：{{missing}}。')
    expect(templateError('{{content')).toBe('变量缺少结束符 }}。')
    expect(templateError('{{}}')).toBe('变量名称不能为空。')
    expect(templateError('前文 }}')).toBe('发现未闭合的变量。')
  })
})
