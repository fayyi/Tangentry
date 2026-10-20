import type { SubtitleAvailability, SubtitleDocument, SubtitlePart, SubtitleTrack } from '../../shared/api/types'

export function selectedSubtitleAvailability(doc: SubtitleDocument | null, track: SubtitleTrack | undefined, origin: 'platform' | 'saved'): SubtitleAvailability | 'available' | 'login_required' | null {
  if (track?.mismatch) return 'ai_mismatch'
  if (origin === 'saved' && track) {
    const firstPart = doc?.parts?.find(part => part.page === doc.part)
    const platformTrack = firstPart?.tracks.find(candidate => candidate.language === track.language && candidate.source === track.source)
    return platformTrack?.mismatch ? 'ai_mismatch' : 'available'
  }
  if (track?.source === 'cc' || track?.source === 'ai') return track.source
  return doc?.status ?? null
}

export function subtitlePartMessage(part: SubtitlePart): { message: string; action?: string; tone: 'neutral' | 'warning' | 'danger' } {
  switch (part.status) {
    case 'cc': return { message: '本分 P 有 CC 字幕。', tone: 'neutral' }
    case 'ai': return { message: '本分 P 有 AI 字幕。', tone: 'neutral' }
    case 'ai_mismatch': return { message: 'AI 字幕与标题疑似不完全匹配，仍可查看，也可以改用转录。', action: '改用转录', tone: 'warning' }
    case 'api_empty': return { message: 'B站返回了字幕轨道，但字幕地址为空。请在工具设置配置或登录 B站后重试。', action: '去账号设置', tone: 'warning' }
    case 'error': return { message: `B站字幕接口读取失败${part.errorCode ? `（${part.errorCode}）` : ''}${part.errorMessage ? `：${part.errorMessage}` : ''}`, action: '重试', tone: 'danger' }
    case 'none': return { message: '本分 P 没有可用字幕，可以直接转录。', action: '转录', tone: 'neutral' }
  }
}

