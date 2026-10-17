const platformNames: Record<string, string> = {
  bilibili: 'B站',
  youtube: 'YouTube',
  xiaohongshu: '小红书',
  douyin: '抖音',
  xiaoyuzhou: '小宇宙',
  rss: 'RSS',
  pdf: 'PDF',
  linuxdo: 'Linux.do',
}

export function platformLabel(platform: string) {
  return platformNames[platform] || platform
}
