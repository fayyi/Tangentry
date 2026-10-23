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

  it('takes an expired server session to sign-in', async () => {
    const assign = vi.fn()
    vi.stubGlobal('window', { location: { assign } })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ code: 'unauthorized', message: '登录已失效' }), { status: 401 }))))

    await expect(request('/library')).rejects.toMatchObject({ code: 'unauthorized', status: 401 })
    expect(assign).toHaveBeenCalledWith('/login')
  })

  it('keeps platform authentication failures on the current page', async () => {
    const assign = vi.fn()
    vi.stubGlobal('window', { location: { assign } })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ code: 'login_required', message: '请登录平台' }), { status: 401 }))))

    await expect(request('/platform')).rejects.toMatchObject({ code: 'login_required', status: 401 })
    expect(assign).not.toHaveBeenCalled()
  })

  it('retains typed failure payloads for backup results', async () => {
    const payload = { code: 'backup_run_failed', message: '上传失败', snapshot: { id: 'snapshot-1', status: 'failed' } }
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(payload), { status: 502 }))))

    try {
      await request('/backup/tasks/task-1/run', { method: 'POST', body: JSON.stringify({ password: 'fixture' }) })
      throw new Error('expected APIError')
    } catch (cause) {
      expect(cause).toBeInstanceOf(APIError)
      expect(cause).toMatchObject({ code: 'backup_run_failed', status: 502, message: '上传失败', payload })
    }
  })
})
