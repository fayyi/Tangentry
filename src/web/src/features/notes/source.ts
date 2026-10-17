// Note source presentation stays local to notes; app owns reading navigation.
export function noteSourceLabel(contentId: string): string {
  if (contentId.startsWith('memo:')) return '随手记'
  if (contentId.startsWith('youtube:')) return `YouTube · ${contentId.slice('youtube:'.length)}`
  if (contentId.startsWith('xiaohongshu:')) return `小红书 · ${contentId.slice('xiaohongshu:'.length)}`
  if (contentId.startsWith('douyin:')) return `抖音 · ${contentId.slice('douyin:'.length)}`
  if (contentId.startsWith('bilibili:')) return `B站 · ${contentId.slice('bilibili:'.length)}`
  if(contentId.startsWith('rss:'))return 'RSS 内容'
  if(contentId.startsWith('pdf:'))return 'PDF 文档'
  if(contentId.startsWith('xiaoyuzhou:'))return '小宇宙节目'
  return contentId
}

export function noteOriginalAt(contentId: string, seconds: number): string | undefined {
  if (/^youtube:[A-Za-z0-9_-]{11}$/.test(contentId)) return `https://www.youtube.com/watch?v=${contentId.slice('youtube:'.length)}&t=${Math.floor(seconds)}s`
  if (/^bilibili:BV[0-9A-Za-z]{10}$/.test(contentId)) return `https://www.bilibili.com/video/${contentId.slice('bilibili:'.length)}/?p=1&t=${Math.floor(seconds)}`
  return undefined
}
