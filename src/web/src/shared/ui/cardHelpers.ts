export interface PlatformMetadata {
  label: string
  colorToken: string
}

export const platformMetadata: Readonly<Record<string, PlatformMetadata>> = {
  bilibili: { label: 'B站', colorToken: '--bilibili-blue' },
  youtube: { label: 'YouTube', colorToken: '--youtube-red' },
  xiaohongshu: { label: '小红书', colorToken: '--xiaohongshu-red' },
  douyin: { label: '抖音', colorToken: '--douyin' },
  xiaoyuzhou: { label: '小宇宙', colorToken: '--xiaoyuzhou' },
  rss: { label: 'RSS', colorToken: '--rss-orange' },
  pdf: { label: 'PDF', colorToken: '--platform-neutral' },
  linuxdo: { label: 'Linux.do', colorToken: '--linuxdo' },
}

const creatorViewPlatforms = new Set(['bilibili', 'youtube', 'linuxdo', 'xiaoyuzhou'])

export function creatorViewSupported(platform: string) {
  return creatorViewPlatforms.has(platform)
}

export function platformLabel(platform: string) {
  return platformMetadata[platform]?.label || platform
}

/** Theme-aware token used by platform labels and their status dots. */
export function platformColorToken(platform: string) {
  return platformMetadata[platform]?.colorToken || '--platform-neutral'
}
