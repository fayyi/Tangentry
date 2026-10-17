import type { PlatformWebView } from '../../shared/api/desktop'

export function supportsInAppLogin(platform: string): platform is PlatformWebView {
  return ['linuxdo', 'douyin', 'bilibili', 'xiaohongshu', 'youtube'].includes(platform)
}

export function showsManualLogin(platform: string, platformWebViewLogin: boolean) {
  if (!platformWebViewLogin) return true
  return platform === 'youtube'
}

export function usesAdvancedManualLogin(platform: string, platformWebViewLogin: boolean) {
  return !platformWebViewLogin && ['bilibili', 'youtube', 'xiaohongshu', 'xiaoyuzhou'].includes(platform)
}
