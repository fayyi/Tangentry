import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIError, request } from './client'

describe('API diagnostics errors', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('keeps structured details while preserving the legacy message and code', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({
      code: 'playback_unavailable',
      message: '抖音视频解析失败：详情接口返回 HTTP 403',
      details: [{ stage: 'webview_detail_fetch', outcome: 'http_error', http_status: 403 }],
    }), { status: 502, headers: { 'Content-Type': 'application/json' } }))))

    await expect(request('/playback/douyin/6961737553342991651')).rejects.toMatchObject({
      code: 'playback_unavailable',
      status: 502,
      message: '抖音视频解析失败：详情接口返回 HTTP 403',
      details: [{ stage: 'webview_detail_fetch', http_status: 403 }],
    })
  })

  it('ignores non-array diagnostic details for older and malformed responses', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ code: 'legacy_error', message: '旧格式错误', details: { stage: 'bad' } }), { status: 503 }))))

    try {
      await request('/legacy')
      throw new Error('expected APIError')
    } catch (cause) {
      expect(cause).toBeInstanceOf(APIError)
      expect(cause).toMatchObject({ code: 'legacy_error', status: 503, message: '旧格式错误', details: [] })
    }
  })
})
