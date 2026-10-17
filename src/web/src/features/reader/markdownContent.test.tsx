import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { isLikelyMarkdown, markdownToPlainText } from './markdownContent'
import MarkdownRenderer from '../notes/MarkdownRenderer'

describe('article markdown detection', () => {
  it('detects common markdown while leaving ordinary prose alone', () => {
    expect(isLikelyMarkdown('**医院诊断为：**\n\n**求助：**')).toBe(true)
    expect(isLikelyMarkdown('这是普通的文章正文，没有标记。')).toBe(false)
    expect(isLikelyMarkdown('<p>HTML 正文</p>')).toBe(false)
  })

  it('keeps visible anchor text and does not interpret raw HTML', () => {
    expect(markdownToPlainText('## 标题\n\n**粗体** 与 [链接](https://example.com)')).toBe('标题\n\n粗体 与 链接')
    expect(isLikelyMarkdown('<script>alert(1)</script>\n**正文**')).toBe(false)
  })

  it('renders safe markdown without executable HTML or event attributes', () => {
    const html = renderToStaticMarkup(<MarkdownRenderer imageMode="inline">{'**正文**\n\n<script>alert(1)</script>\n\n<img src="x" onerror="alert(1)">'}</MarkdownRenderer>)
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('<img')
    expect(html).toContain('<strong>正文</strong>')
  })
})
