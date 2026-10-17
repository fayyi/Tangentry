import { expect, test } from 'vitest'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ReactMarkdown from 'react-markdown'
import { transcriptMarkdown } from './transcriptMarkdown'
import type { SubtitleTrack } from '../../shared/api/types'

const videoId = 'bilibili:BVstream0000'
const readerURL = 'http://127.0.0.1:7391/reader?contentId=' + encodeURIComponent(videoId)
const track: SubtitleTrack = { language: 'en', label: 'English', automatic: false, cues: [
  { start: 0, end: 3.863, text: 'First source line\nSecond source line😀' },
  { start: 9258.100000000364, end: 9260.21, text: 'Final source line' },
] }

test('Markdown preserves exact timings, multilingual multiline text, modes and first-part video links', () => {
  const source = { contentId: videoId, title: '导出测试', author: '来源作者', url: 'https://www.bilibili.com/video/BVstream0000/?p=2&keep=1' }
  const before = JSON.stringify(track)
  const output = transcriptMarkdown(track, source, readerURL, 'bilingual', ['第一段译文', ''])
  expect(output).toContain('# 导出测试')
  expect(output).toContain('作者：来源作者')
  expect(output).toContain('已翻译 1 / 2 段')
  expect(output).toContain('### 1 · 00:00:00.000 – 00:00:03.863')
  expect(output).toContain('时间：9258.100000000364–9260.21 秒')
  expect(output).toContain('02:34:18.100')
  expect(output).toContain('> First source line\n> Second source line😀')
  expect(output).toContain('> 第一段译文')
  expect(output).toContain('> Final source line')
  expect(output).toContain('?p=1&keep=1&t=0')
  expect(output).toContain('?p=1&keep=1&t=9258')
  expect(output).toContain('[在 Tangentry 查看](<' + readerURL + '>)')
  expect(JSON.stringify(track)).toBe(before)
  expect(transcriptMarkdown(track, source, readerURL, 'original', ['译文'])).not.toContain('> 译文')
  expect(() => transcriptMarkdown(track, source, readerURL, 'translated', ['译文'])).toThrow('译文尚未完成')
  const translated = transcriptMarkdown(track, source, readerURL, 'translated', ['第一段译文', '最后译文'])
  expect(translated).not.toContain('First source')
  expect(translated).toContain('> 最后译文')
  const identical = transcriptMarkdown(track, source, readerURL, 'bilingual', [track.cues[0].text])
  expect(identical.match(/> First source line/g)).toHaveLength(1)
})

test('Markdown adds time links only to matching supported video sources and preserves podcast source URLs', () => {
  const youtube = transcriptMarkdown(track, { contentId: 'youtube:abcdefghijk', url: 'https://www.youtube.com/watch?v=abcdefghijk&list=keep' }, readerURL)
  expect(youtube).toContain('&list=keep&t=0s')
  for (const source of [
    { contentId: 'youtube:abcdefghijk', url: 'https://www.youtube.com/watch?v=other' },
    { contentId: videoId, url: 'https://www.bilibili.com/video/BVdifferent/' },
    { contentId: 'xiaoyuzhou:' + 'a'.repeat(24), url: 'https://www.xiaoyuzhoufm.com/episode/' + 'a'.repeat(24) + '?keep=1' },
    { contentId: 'rss:' + 'b'.repeat(64), url: 'https://podcast.example/episode?keep=1' },
  ]) {
    const output = transcriptMarkdown(track, source, readerURL)
    expect(output).toContain('[原内容](<' + source.url + '>)')
    expect(output).not.toContain('跳转原视频')
    expect(output).toContain('时间：0–3.863 秒')
  }
})

test('Markdown treats source and translation markup as text and excludes unsafe addresses', () => {
  const dangerous = '![图片](https://evil.example/image) <script>alert(1)</script>\n# Heading\n---\n- [ ] Task\n[click](javascript:alert(1)) & entity'
  const input = { ...track, label: dangerous, cues: [{ start: 0, end: 1, text: dangerous }] }
  for (const url of ['javascript:alert(1)', 'data:text/html,secret', 'https://user:password@private.example/story']) {
    const output = transcriptMarkdown(input, { contentId: videoId, title: dangerous, author: dangerous, url }, readerURL, 'bilingual', [dangerous + ' 译文'])
    const html = renderToStaticMarkup(createElement(ReactMarkdown, { children: output }))
    expect(html).not.toMatch(/<(?:script|img|input|hr|li)\b/)
    expect(html.match(/<h1>/g)).toHaveLength(1)
    expect(html).not.toContain('href="javascript:')
    expect(output).not.toContain('private.example')
    expect(output).not.toContain('secret')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
  }
  const url = 'https://example.test/story?q=<img>&keep=1'
  const rendered = renderToStaticMarkup(createElement(ReactMarkdown, { children: transcriptMarkdown(track, { contentId: videoId, url }, readerURL) }))
  expect(rendered).toContain('%3Cimg%3E')
  expect(rendered).not.toContain('<img')
  for (const invalid of [
    { ...track, cues: [] },
    { ...track, cues: [{ start: NaN, end: Infinity, text: 'text' }] },
    { ...track, cues: [{ start: -1, end: 1, text: 'text' }] },
    { ...track, cues: [{ start: 2, end: 1, text: 'text' }] },
    { ...track, cues: [{ start: 0, end: 1, text: '' }] },
  ]) expect(() => transcriptMarkdown(invalid, { contentId: videoId, url: '' }, readerURL)).toThrow()
  expect(() => transcriptMarkdown(track, { contentId: videoId, url: '' }, 'javascript:alert(1)')).toThrow()
})
