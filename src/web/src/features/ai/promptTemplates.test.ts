import { describe, expect, it } from 'vitest'
import { EMPTY_TEMPLATES, changedTemplateFields, insertVariable, renderTemplate, resetTemplate, templateError, templatesEqual } from './promptTemplates'

const defaults = {
  summary: { system: '摘要系统', user: '{{content}}' },
  translation: { system: '翻译系统', user: '{{content}}' },
  chat: { system: '对话系统', user: '{{content}}' },
}

describe('prompt template helpers', () => {
  it('compares defaults and reports changed fields', () => {
    expect(templatesEqual(defaults.summary, defaults.summary)).toBe(true)
    expect(changedTemplateFields({ ...defaults.summary, user: '自定义 {{content}}' }, 'summary', defaults)).toEqual(['用户提示'])
  })

  it('resets only the selected task', () => {
    const edited = { ...defaults, summary: { ...defaults.summary, user: '自定义摘要' }, chat: { ...defaults.chat, system: '自定义对话' } }
    const reset = resetTemplate(edited, 'summary', defaults)
    expect(reset.summary).toEqual(defaults.summary)
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
    expect(templateError('前文 }}')).toBeNull()
    expect(EMPTY_TEMPLATES.summary).toEqual({ system: '', user: '' })
  })
})
