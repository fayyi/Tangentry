import { describe, expect, it } from 'vitest'
import { diagnosticOutcomeName, diagnosticStageFacts, diagnosticStageName, parseDouyinDiagnostics } from './douyinDiagnostics'

describe('Douyin playback diagnostics', () => {
  it('maps passive-capture failure reasons while keeping response snippets structurally redacted', () => {
    const [stage, unsafe] = parseDouyinDiagnostics([
      { stage: 'webview_detail_capture', outcome: 'exception', reason: 'observer_not_installed', response_snippet_redacted: '{"status_code":403,"message":"[文本已脱敏]"}' },
      { stage: 'webview_detail_capture', outcome: 'exception', reason: 'fetch_reject', response_snippet_redacted: '{"message":"token=private"}' },
    ])
    expect(stage).toBeDefined()
    expect(diagnosticStageFacts(stage!)).toContain('页面请求观察器未安装')
    expect(stage?.response_snippet_redacted).toBe('{"status_code":403,"message":"[文本已脱敏]"}')
    expect(unsafe?.response_snippet_redacted).toBeNull()
    expect(JSON.stringify([stage, unsafe])).not.toContain('token=private')
  })

  it('sorts stages and sanitizes URLs and stderr', () => {
    const values = parseDouyinDiagnostics([
      { stage: 'ytdlp', outcome: 'exception', ytdlp_exit_code: 1, ytdlp_stderr_first_error_redacted: 'ERROR: https://fixture.example/path?token=private' },
      { stage: 'webview_detail_fetch', outcome: 'http_error', http_status: 403, content_type: 'application/json; charset=utf-8', page_url: 'https://www.douyin.com/aweme/v1/web/aweme/detail/?a_bogus=fixture' },
      { stage: 'ignored', outcome: 'exception' },
    ])
    expect(values.map(stage => stage.stage)).toEqual(['webview_detail_fetch', 'ytdlp'])
    expect(values[0]?.page_url).toBe('www.douyin.com/aweme/v1/web/aweme/detail/')
    expect(values[0]?.content_type).toBe('application/json')
    expect(values[1]?.ytdlp_stderr_first_error_redacted).toBe('ERROR: fixture.example/path')
    expect(JSON.stringify(values)).not.toMatch(/token=private|a_bogus|\?token=/)
  })

  it('maps phase and outcome labels and exposes only safe facts', () => {
    const [stage] = parseDouyinDiagnostics([{ stage: 'webview_detail_fetch', outcome: 'too_large', http_status: 200, is_douyin_context: false }])
    expect(stage).toBeDefined()
    expect(diagnosticStageName(stage!.stage)).toBe('详情接口')
    expect(diagnosticOutcomeName(stage!.outcome)).toBe('响应过大')
    expect(diagnosticStageFacts(stage!)).toEqual(['HTTP 200', '非抖音页面上下文'])
  })

  it('shows page readiness, blocked navigation, player code and retry count without unsafe text', () => {
    const [stage] = parseDouyinDiagnostics([{
      stage: 'frontend_media', outcome: 'exception', frontend_media_code: 2, attempted: 1,
      frontend_message: 'ERROR https://fixture.example/media.mp4?token=private a_bogus=signature-private',
    }])
    expect(stage).toBeDefined()
    expect(diagnosticStageFacts(stage!)).toEqual(['自动重试 1 次', '媒体错误码 2', 'ERROR fixture.example/media.mp4 a_bogus=[已脱敏]'])
    expect(JSON.stringify(stage)).not.toMatch(/token=private|signature-private|\?token=/)
  })

  it('keeps the final blocked host and path while dropping its query', () => {
    const [stage] = parseDouyinDiagnostics([{
      stage: 'webview_page_load', outcome: 'restricted', page_ready_state: 'complete',
      blocked_navigation_url: 'https://passport.douyin.com/login?next=private',
    }])
    expect(diagnosticStageFacts(stage!)).toEqual(['passport.douyin.com/login', '已阻止导航：passport.douyin.com/login', '页面状态 complete'])
    expect(JSON.stringify(stage)).not.toContain('next=private')
  })

  it('redacts sensitive path segments and limits Douyin CDN paths', () => {
    const hosts = ['v1.douyinvod.com', 'cdn.douyinvod.net', 'media.bytecdn.cn', 'v2.zjcdn.com', 'v3.amemv.com']
    const [page, ...cdn] = parseDouyinDiagnostics([
      { stage: 'webview_page_load', outcome: 'ok', page_url: 'https://www.douyin.com/video/abcdefghijklmnopqrstuvwxyzABCDEF/detail' },
      ...hosts.map(host => ({ stage: 'session_media_fetch', outcome: 'ok', page_url: `https://${host}/video/segment/abcdefghijklmnopqrstuvwxyzABCDEF/signed.mp4?signature=fixture` })),
    ])
    expect(page?.page_url).toBe('www.douyin.com/video/<redacted>/detail')
    expect(cdn.map(stage => stage.page_url)).toEqual(hosts.map(host => `${host}/video/segment`))
    expect(JSON.stringify([page, cdn])).not.toMatch(/abcdefghijklmnopqrstuvwxyzABCDEF|signature=fixture/)
  })

  it('keeps the lead-approved cookie configuration boolean without exposing content', () => {
    const [stage] = parseDouyinDiagnostics([{ stage: 'ytdlp', outcome: 'exception', cookie_configured: true }])
    expect(diagnosticStageFacts(stage!)).toContain('Cookie 已配置：是')
    expect(JSON.stringify(stage)).not.toContain('cookie-secret')
  })
})
