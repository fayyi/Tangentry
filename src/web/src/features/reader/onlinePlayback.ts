export function hasPlayableOnlineSource(mediaUrl?: string, compatibilitySource?: string): boolean {
  return Boolean(mediaUrl || compatibilitySource)
}
