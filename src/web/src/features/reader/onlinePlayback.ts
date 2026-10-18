export function hasPlayableOnlineSource(mediaUrl?: string, compatibilitySource?: string): boolean {
  return Boolean(mediaUrl || compatibilitySource)
}

export function shouldStartCompatibilityPlayback(platform?: string): boolean {
  return platform === 'xiaohongshu'
}
