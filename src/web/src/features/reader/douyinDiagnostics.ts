export type DiagnosticOutcome = 'ok' | 'restricted' | 'timeout' | 'too_large' | 'no_url' | 'http_error' | 'exception'

export type DouyinDiagnosticStage = {
  stage: string
  outcome: DiagnosticOutcome
  http_status: number | null
  content_type: string | null
  page_url: string | null
  blocked_navigation_url: string | null
  page_ready_state: string | null
  is_douyin_context: boolean | null
  ytdlp_version: string | null
  ytdlp_exit_code: number | null
  ytdlp_stderr_first_error_redacted: string | null
  reason: string | null
  api_code: string | null
  business_status: number | null
  candidate_count: number | null
  accepted_count: number | null
  video_element_count: number | null
  attempted: number | null
  refreshed: boolean | null
  cookie_configured: boolean | null
  frontend_media_code: number | null
  frontend_message: string | null
  bytes_read: number | null
  response_bytes: number | null
  detail_url_count: number | null
  video_source_count: number | null
  protocol: string | null
  extension: string | null
  video_codec: string | null
  audio_codec: string | null
  duration_seconds: number | null
  cdn_allowed: boolean | null
  signature_deadline_unix: number | null
}

const outcomes = new Set<DiagnosticOutcome>(['ok', 'restricted', 'timeout', 'too_large', 'no_url', 'http_error', 'exception'])
const stageOrder = ['frontend_request', 'playback_open', 'webview_session', 'webview_page_load', 'webview_detail_fetch', 'webview_media_extract', 'resolver_fallback', 'ytdlp', 'source_validate', 'session_media_fetch', 'frontend_media']
const stageNames: Record<string, string> = {
  frontend_request: '前端请求', playback_open: '播放解析', webview_session: 'WebView 会话', webview_page_load: 'WebView 页面',
  webview_detail_fetch: '详情接口', webview_media_extract: '媒体地址提取', resolver_fallback: '解析回退', ytdlp: 'yt-dlp',
  source_validate: '播放地址校验', session_media_fetch: '媒体请求', frontend_media: '播放器',
}
const outcomeNames: Record<DiagnosticOutcome, string> = {
  ok: '成功', restricted: '需要验证', timeout: '超时', too_large: '响应过大', no_url: '没有可用地址', http_error: 'HTTP 错误', exception: '异常',
}
const reasonNames: Record<string, string> = {
  request_sent: '请求已发出', id_validated: '视频标识有效', invalid_id: '视频标识无效', concurrency_limit: '并发数量已达限制',
  resolver_started: '开始解析', resolver_success: '解析成功', resolver_error: '解析器失败', resolver_timeout: '解析器超时',
  fallback_allowed: '允许回退到 yt-dlp', fallback_blocked_restricted: '页面验证，已阻止 yt-dlp 回退', fallback_blocked_timeout: '页面超时，已阻止 yt-dlp 回退', fallback_blocked_cancelled: '请求已取消，已阻止 yt-dlp 回退',
  webview_success: '应用内页面解析成功', webview_no_url: '应用内页面没有媒体地址', session_created: '新建会话', session_reused: '复用会话', session_destroyed: '会话不可用', dom_ready: '页面桥接就绪',
  navigation_blocked: '导航被安全策略阻止', page_load_error: '页面加载失败', javascript_error: '页面脚本执行失败', network_error: '网络请求失败',
  detail_success: '详情读取成功', http_status: 'HTTP 请求失败', business_verification: '业务验证状态', non_json: '响应不是 JSON', invalid_json: 'JSON 格式异常', response_too_large: '响应超过大小限制',
  media_found: '找到媒体候选', media_not_found: '没有媒体候选', candidate_filtered: '候选不符合白名单或已过期', candidate_expired: '候选已过期', cdn_rejected: 'CDN 不在白名单', source_expired: '签名已过期', source_valid: '播放地址有效',
  tool_unavailable: 'yt-dlp 不可用', cookie_read_error: 'Cookie 配置读取失败', cookie_configured: 'Cookie 已配置', command_error: 'yt-dlp 命令创建失败', command_failed: 'yt-dlp 执行失败', cancelled: '请求已取消', stdout_too_large: 'yt-dlp 输出过大',
  format_invalid: '播放格式不兼容', json_invalid: 'yt-dlp 输出格式异常', timeout: '等待超时', signature_refresh: '刷新签名地址', candidate_http_error: '媒体地址请求失败', invalid_media_response: '媒体响应格式不兼容',
  stream_error: '媒体流读取失败', media_aborted: '播放已中止', media_decode_error: '媒体解码失败', media_network_error: '播放器网络错误', media_src_not_supported: '媒体格式不受支持', api_error: 'API 请求失败', request_error: '请求失败',
}

function nullableString(value: unknown, max = 320): string | null {
  if (typeof value !== 'string' || value.length > max) return null
  const trimmed = value.trim()
  return trimmed ? trimmed : null
}
function nullableNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isSafeInteger(value) ? value : null
}
function nullableBoolean(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null
}

export function safeDiagnosticURL(value: unknown): string | null {
  const raw = nullableString(value, 2048)
  if (!raw) return null
  try {
    const url = new URL(raw.includes('://') ? raw : `https://${raw}`)
    if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || (url.port && !['80', '443'].includes(url.port))) return null
    return `${url.hostname.toLowerCase()}${url.pathname}`.slice(0, 1100)
  } catch {
    return null
  }
}

export function safeDiagnosticText(value: unknown): string | null {
  let text = nullableString(value, 4096)
  if (!text) return null
  text = text.replace(/https?:\/\/[^\s"'<>]+/gi, raw => {
    const suffix = raw.match(/[.,;:!?)\]}]+$/)?.[0] ?? ''
    const url = safeDiagnosticURL(raw.slice(0, raw.length - suffix.length))
    return `${url ?? '[URL]'}${suffix}`
  })
  text = text.replace(/\b(cookie|set-cookie)\b\s*[:=]\s*[^\r\n]*/gi, '$1=[已脱敏]')
  text = text.replace(/\b(authorization|proxy-authorization)\b\s*[:=]\s*[^\s,;&]+(?:\s+[^\s,;&]+)?/gi, '$1=[已脱敏]')
  text = text.replace(/\b(cookie|set-cookie|authorization|proxy-authorization|token|msToken|a_bogus|signature|sign)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;&]+)/gi, '$1=[已脱敏]')
  text = text.replace(/\bBearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [已脱敏]')
  text = text.replace(/\b[A-Za-z0-9_-]{28,}\b/g, '[长字符串已脱敏]')
  text = text.replace(/\b[0-9]{12,}\b/g, '[长数字已脱敏]')
  return text.slice(0, 320)
}

export function parseDouyinDiagnostics(value: unknown): DouyinDiagnosticStage[] {
  if (!Array.isArray(value)) return []
  return value.flatMap(item => {
    if (!item || typeof item !== 'object') return []
    const raw = item as Record<string, unknown>
    if (typeof raw.stage !== 'string' || !stageNames[raw.stage] || typeof raw.outcome !== 'string' || !outcomes.has(raw.outcome as DiagnosticOutcome)) return []
    return [{
      stage: raw.stage, outcome: raw.outcome as DiagnosticOutcome,
      http_status: nullableNumber(raw.http_status),
      content_type: nullableString(raw.content_type, 128)?.split(';', 1)[0].toLowerCase() ?? null,
      page_url: safeDiagnosticURL(raw.page_url),
      blocked_navigation_url: safeDiagnosticURL(raw.blocked_navigation_url),
      page_ready_state: nullableString(raw.page_ready_state, 16),
      is_douyin_context: nullableBoolean(raw.is_douyin_context),
      ytdlp_version: nullableString(raw.ytdlp_version, 80),
      ytdlp_exit_code: nullableNumber(raw.ytdlp_exit_code),
      ytdlp_stderr_first_error_redacted: safeDiagnosticText(raw.ytdlp_stderr_first_error_redacted),
      reason: nullableString(raw.reason, 40), api_code: nullableString(raw.api_code, 64),
      business_status: nullableNumber(raw.business_status), candidate_count: nullableNumber(raw.candidate_count),
      accepted_count: nullableNumber(raw.accepted_count), video_element_count: nullableNumber(raw.video_element_count),
      attempted: nullableNumber(raw.attempted), refreshed: nullableBoolean(raw.refreshed),
      cookie_configured: nullableBoolean(raw.cookie_configured), frontend_media_code: nullableNumber(raw.frontend_media_code),
      frontend_message: safeDiagnosticText(raw.frontend_message), bytes_read: nullableNumber(raw.bytes_read), response_bytes: nullableNumber(raw.response_bytes),
      detail_url_count: nullableNumber(raw.detail_url_count), video_source_count: nullableNumber(raw.video_source_count), protocol: nullableString(raw.protocol, 8),
      extension: nullableString(raw.extension, 16), video_codec: nullableString(raw.video_codec, 32),
      audio_codec: nullableString(raw.audio_codec, 32), duration_seconds: nullableNumber(raw.duration_seconds),
      cdn_allowed: nullableBoolean(raw.cdn_allowed), signature_deadline_unix: nullableNumber(raw.signature_deadline_unix),
    }]
  }).sort((a, b) => stageOrder.indexOf(a.stage) - stageOrder.indexOf(b.stage))
}

export function diagnosticStageName(stage: string): string { return stageNames[stage] ?? '播放阶段' }
export function diagnosticOutcomeName(outcome: DiagnosticOutcome): string { return outcomeNames[outcome] }

export function diagnosticStageFacts(stage: DouyinDiagnosticStage): string[] {
  const facts: string[] = []
  if (stage.http_status !== null) facts.push(`HTTP ${stage.http_status}`)
  if (stage.content_type) facts.push(stage.content_type)
  if (stage.page_url) facts.push(stage.page_url)
  else if (stage.blocked_navigation_url) facts.push(stage.blocked_navigation_url)
  if (stage.blocked_navigation_url) facts.push(`已阻止导航：${stage.blocked_navigation_url}`)
  if (stage.page_ready_state) facts.push(`页面状态 ${stage.page_ready_state}`)
  if (stage.is_douyin_context !== null) facts.push(stage.is_douyin_context ? '抖音页面上下文' : '非抖音页面上下文')
  if (stage.reason && reasonNames[stage.reason]) facts.push(reasonNames[stage.reason])
  if (stage.business_status !== null) facts.push(`业务状态 ${stage.business_status}`)
  if (stage.candidate_count !== null) facts.push(`候选 ${stage.candidate_count}`)
  if (stage.accepted_count !== null) facts.push(`白名单通过 ${stage.accepted_count}`)
  if (stage.video_element_count !== null) facts.push(`video 元素 ${stage.video_element_count}`)
  if (stage.detail_url_count !== null) facts.push(`详情地址 ${stage.detail_url_count}`)
  if (stage.video_source_count !== null) facts.push(`video 地址 ${stage.video_source_count}`)
  if (stage.response_bytes !== null) facts.push(`响应 ${stage.response_bytes} 字节`)
  if (stage.stage === 'ytdlp') {
    facts.push(`版本 ${stage.ytdlp_version ?? '未知'}`)
    facts.push(`退出码 ${stage.ytdlp_exit_code ?? '无'}`)
    if (stage.ytdlp_stderr_first_error_redacted) facts.push(stage.ytdlp_stderr_first_error_redacted)
    if (stage.cookie_configured !== null) facts.push(`Cookie 已配置：${stage.cookie_configured ? '是' : '否'}`)
  }
  if (stage.refreshed !== null) facts.push(`刷新地址：${stage.refreshed ? '是' : '否'}`)
  if (stage.stage === 'frontend_media' && stage.attempted !== null) facts.push(`自动重试 ${stage.attempted} 次`)
  if (stage.frontend_media_code !== null) facts.push(`媒体错误码 ${stage.frontend_media_code}`)
  if (stage.frontend_message) facts.push(stage.frontend_message)
  if (stage.bytes_read !== null) facts.push(`已读取 ${stage.bytes_read} 字节`)
  if (stage.protocol) facts.push(stage.protocol)
  if (stage.extension) facts.push(stage.extension)
  if (stage.video_codec) facts.push(`视频 ${stage.video_codec}`)
  if (stage.audio_codec) facts.push(`音频 ${stage.audio_codec}`)
  if (stage.duration_seconds !== null) facts.push(`${stage.duration_seconds} 秒`)
  if (stage.cdn_allowed !== null) facts.push(`CDN 白名单：${stage.cdn_allowed ? '通过' : '拒绝'}`)
  return facts
}
