export const discoverySources = {
  bilibili: { label: 'B站', description: 'UP 主投稿或单条视频', placeholder: 'UP 主 UID、主页地址或视频 BV 号', inputLabel: 'UP 主 UID 或视频地址' },
  youtube: { label: 'YouTube', description: '粘贴单视频地址', placeholder: 'youtube.com/watch 或 youtu.be 视频链接', inputLabel: 'YouTube 视频地址' },
  douyin: { label: '抖音', description: '单视频或分享链接；受限时需配置抖音 Cookie', placeholder: 'https://www.douyin.com/video/… 或 v.douyin.com 分享链接', inputLabel: '抖音视频或分享地址' },
  xiaohongshu: { label:'小红书',description:'单条图文或视频，保留完整分享地址',placeholder:'xiaohongshu.com/explore/… 或 xhslink.com 分享链接',inputLabel:'小红书笔记或分享地址' },
  rss: { label: 'RSS', description: 'RSS / Atom 文章与播客', placeholder: 'https://example.com/feed.xml', inputLabel: 'RSS 订阅地址' },
  xiaoyuzhou: { label: '小宇宙', description: '公开单集与节目说明', placeholder: 'xiaoyuzhoufm.com/episode/… 的完整地址', inputLabel: '小宇宙单集地址' },
  pdf: { label: 'PDF', description: '文档链接或本机文件', placeholder: 'https://example.com/document.pdf', inputLabel: 'PDF 文档地址' },
}
export type DiscoverySource = keyof typeof discoverySources
