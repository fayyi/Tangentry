import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { DiscussionFallback } from './DiscussionFallback'

describe('DiscussionFallback', () => {
  it('keeps the saved body visible while loading missing floors', () => {
    const html = renderToStaticMarkup(createElement(DiscussionFallback, {
      loading: true,
      error: '',
      onRetry: () => {},
      children: createElement('article', null, '已保存的主帖正文'),
    }))

    expect(html).toContain('已保存的主帖正文')
    expect(html).toContain('正在加载话题楼层')
  })

  it('shows the detail error and offers retry without hiding the body', () => {
    const retry = vi.fn()
    const html = renderToStaticMarkup(createElement(DiscussionFallback, {
      loading: false,
      error: 'Linux.do 暂时不可用',
      onRetry: retry,
      children: createElement('article', null, '旧正文'),
    }))

    expect(html).toContain('旧正文')
    expect(html).toContain('Linux.do 暂时不可用')
    expect(html).toContain('重试')
  })

  it('offers in-app verification when the platform requires it', () => {
    const html = renderToStaticMarkup(createElement(DiscussionFallback, {
      loading: false,
      error: '需要在应用内完成 Linux.do 验证',
      onRetry: () => {},
      onVerify: () => {},
      children: createElement('article', null, '旧正文'),
    }))

    expect(html).toContain('重新验证')
  })
})
