import { expect, test } from 'vitest'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ReactMarkdown from 'react-markdown'
import { summaryMarkdown } from './summaryMarkdown'
import type { SummaryResult } from './summaryTypes'

const videoId = 'bilibili:BVstream0000'
const videoURL = 'https://www.bilibili.com/video/BVstream0000/?p=2'
const readerURL = 'http://127.0.0.1:7391/reader?contentId=' + encodeURIComponent(videoId)
function saved(overrides: Partial<SummaryResult> = {}): SummaryResult {
  return {
    key: 'summary-export', contentId: videoId, source: 'subtitle',
    baseUrl: 'https://private-ai.example/v1', model: 'private-model',
    complete: true, totalCalls: 1, completedCalls: 1, updatedAt: 1720000000,
    overview: '从原文核对摘要。', points: [{ text: '开头的要点', indices: [0] }],
    citations: [{ index: 0, start: 0, end: 3.863, quote: '第一行原文。\n第二行原文😀。' }],
    ...overrides,
  }
}

test('summary Markdown retains zero and fractional times, repeated evidence and complete multiline quotations', () => {
  const output = summaryMarkdown(saved({
    points: [{ text: '第一点', indices: [0, 7] }, { text: '第二点', indices: [7] }],
    citations: [...saved().citations!, { index: 7, start: 9258.100000000364, end: 9260.21, quote: '完整证据'.repeat(80) }],
  }), { contentId: videoId, title: '摘要来源', url: videoURL }, readerURL)
  expect(output).toContain('# 摘要来源')
  expect(output).toContain('引用：[1] [8]')
  expect(output).toContain('引用：[8]')
  expect(output.match(/### \[8\]/g)).toHaveLength(1)
  expect(output).toContain('### [1] · 0–3.863 秒')
  expect(output).toContain('### [8] · 9258.100000000364–9260.21 秒')
  expect(output).toContain('> 第一行原文。\n> 第二行原文😀。')
  expect(output).toContain('> ' + '完整证据'.repeat(80))
  expect(output).toContain('[原内容](<https://www.bilibili.com/video/BVstream0000/?p=2>)')
  expect(output).toContain('https://www.bilibili.com/video/BVstream0000/?p=1&t=0')
  expect(output).toContain('https://www.bilibili.com/video/BVstream0000/?p=1&t=9258')
  expect(output).not.toContain('private-ai')
  expect(output).not.toContain('private-model')
})

test('summary Markdown links only known video timestamps and preserves other original source URLs', () => {
  const result = saved({ citations: [{ index: 0, start: 3.863, end: 6.21, quote: '原文' }] })
  const youtube = summaryMarkdown(result, { contentId: 'youtube:abcdefghijk', url: 'https://www.youtube.com/watch?v=abcdefghijk&list=keep' }, readerURL)
  expect(youtube).toContain('https://www.youtube.com/watch?v=abcdefghijk&list=keep&t=3s')
  const podcast = summaryMarkdown(result, { contentId: videoId, url: 'https://www.xiaoyuzhoufm.com/episode/abc?keep=1' }, readerURL)
  expect(podcast).toContain('[打开原内容](<https://www.xiaoyuzhoufm.com/episode/abc?keep=1>)')
  expect(podcast).not.toContain('&t=')
})

test('summary timestamps never target another video or a lookalike platform path', () => {
  const result = saved({ citations: [{ index: 0, start: 3.863, end: 6.21, quote: '原文' }] })
  for (const [contentId, url] of [
    [videoId, 'https://www.youtube.com/watch?v=abcdefghijk'],
    ['youtube:abcdefghijk', 'https://www.youtube.com/watch?v=other000001'],
    [videoId, 'https://www.bilibili.com/video/BVother00000/?p=2'],
    [videoId, 'https://www.bilibili.com/video/BVstream0000/extra'],
    [videoId, 'https://www.bilibili.com.evil.example/video/BVstream0000/'],
  ]) {
    const output = summaryMarkdown(result, { contentId, url }, readerURL)
    expect(output).toContain(`[打开原内容](<${url}>)`)
    expect(output).not.toMatch(/[?&]t=/)
  }
})

test('summary Markdown treats model and source strings as text, excludes unsafe links and renders no injected elements', () => {
  const dangerous = '![图片](https://evil.example/image) <script>alert(1)</script>\n# 标题\n---\n- 项目\n[点击](javascript:alert(1)) & entity'
  const result = saved({ overview: dangerous, points: [{ text: dangerous, indices: [0] }], citations: [{ index: 0, quote: dangerous }] })
  for (const url of ['javascript:alert(1)', 'data:text/html,secret', 'https://user:secret@private.example/story']) {
    const output = summaryMarkdown(result, { contentId: videoId, title: dangerous, url }, readerURL)
    const html = renderToStaticMarkup(createElement(ReactMarkdown, { children: output }))
    expect(html).not.toMatch(/<(?:script|img|hr)\b/)
    expect(html.match(/<h1>/g)).toHaveLength(1)
    expect(html.match(/<li>/g)).toHaveLength(1)
    expect(html).not.toContain('href="javascript:')
    expect(output).not.toContain('private.example')
    expect(output).not.toContain('secret')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
  }
  expect(() => summaryMarkdown(saved(), { contentId: videoId }, 'javascript:alert(1)')).toThrow('阅读地址无效')
})

test('summary export refuses incomplete summaries and missing, duplicate or invalid evidence', () => {
  const source = { contentId: videoId }
  for (const result of [
    saved({ complete: false }), saved({ overview: '' }), saved({ points: [] }),
    saved({ points: [{ text: '无引用', indices: [] }] }),
    saved({ points: [{ text: '缺引用', indices: [2] }] }),
    saved({ points: [{ text: '', indices: [0] }] }),
    saved({ points: [{ text: '重复编号', indices: [0, 0] }] }),
    saved({ citations: [{ index: 0, quote: ' ' }] }),
    saved({ citations: [{ index: 0, quote: '甲' }, { index: 0, quote: '乙' }] }),
    saved({ citations: [{ index: -1, quote: '负数编号' }] }),
    saved({ citations: [{ index: 0.5, quote: '非整数编号' }] }),
    saved({ citations: [{ index: 0, start: 0, quote: '缺结束时间' }] }),
    saved({ citations: [{ index: 0, end: 3, quote: '缺开始时间' }] }),
    saved({ citations: [{ index: 0, start: -1, end: 3, quote: '负数时间' }] }),
    saved({ citations: [{ index: 0, start: 3, end: 3, quote: '无效范围' }] }),
    saved({ citations: [{ index: 0, start: NaN, end: Infinity, quote: '无效时间' }] }),
  ]) expect(() => summaryMarkdown(result, source, readerURL)).toThrow()
})

test('summary Markdown exports the TextFold content tree with all levels and source locations', () => {
  const result = saved({
    document: {
      formatVersion: 1,
      topic: '系统化阅读 · 章节地图',
      levels: ['Topic', 'Takeaway', 'Key', 'Brief', 'Full'],
      sections: [{
        sectionId: 'section-1', title: '章节一', start: 12.5, end: 18.25,
        topic: '系统化阅读', takeaway: '先建立地图再深入。',
        evidence: [{ id: 'e1', quote: '章节证据', start: 12.5, end: 18.25 }],
        units: [{
          unitId: 'unit-1', topic: '地图', takeaway: '先建立地图。', key: '地图降低搜索成本。',
          briefSentenceIds: ['sentence-1'],
          sentences: [{ id: 'sentence-1', text: '原文关键句。', evidence: [{ id: 'e2', quote: '原文关键句。', start: 13, end: 14 }] }],
          full: '原文关键句。\n原文补充句。',
        }],
      }],
    },
  })
  const output = summaryMarkdown(result, { contentId: videoId, title: '内容树', url: videoURL }, readerURL)
  expect(output).toContain('系统化阅读 · 章节地图')
  expect(output).toContain('### 章节一')
  expect(output).toContain('**Takeaway**：先建立地图。')
  expect(output).toContain('**Key**：地图降低搜索成本。')
  expect(output).toContain('**Brief**：原文关键句。')
  expect(output).toContain('**Full**：原文关键句。')
  expect(output).toContain('12.5–18.25 秒')
  expect(output).toContain('https://www.bilibili.com/video/BVstream0000/?p=1&t=12')
  expect(output).toContain('章节证据')
})
