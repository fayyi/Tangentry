export interface ArticleBlock {
  kind: 'paragraph' | 'heading' | 'quote' | 'code' | 'list' | 'image'
  level?: 2 | 3 | 4
  text: string
  url?: string
  alt?: string
  width?: number
  height?: number
}
export interface ArticleDocument {
  contentId: string
  sourceUrl: string
  title: string
  byline: string
  blocks: ArticleBlock[]
  text: string
  sourceHash: string
  revision: number
  fetchedAt: number
}

export interface ContentItem {
	images?: {url:string;width?:number;height?:number;alt?:string}[]
	articleBlocks?: ArticleBlock[]
	// A library card without its body; read the full item before saving it.
	partial?: boolean
  id: string
  platform: string
  platformId: string
  kind: 'video' | 'article' | 'audio' | 'pdf' | 'image'
  mediaUrl?: string
  title: string
  author: string
  authorId: string
  authorAvatarUrl?: string
  url: string
  thumbnailUrl: string
  description: string
  previewText?: string
  durationSeconds: number
  publishedAt: number
  publishedText?: string
  savedAt?: number
}

export interface Feed {
  source?: string
  items: ContentItem[]
  page: number
  total: number
  hasMore: boolean
  sourceTitle: string
  query: string
}

export interface DownloadOptions {
  mode?: 'video' | 'audio'
  container?: 'mp4' | 'mkv'
  videoOnly?: boolean
  audioFormat?: 'best' | 'mp3' | 'm4a' | 'flac' | 'wav' | 'opus'
  videoFormatId?: string
  audioFormatId?: string
  subtitleLanguages?: string[]
  automaticSubtitles?: boolean
  compress?: boolean
}

export interface DownloadFormat {
  id: string
  ext: string
  height: number
  width: number
  fps: number
  videoCodec: string
  audioCodec: string
  audioBitrate: number
  fileSize: number
  approximate: boolean
}

export interface DownloadMediaInfo {
  url: string
  title: string
  author: string
  thumbnailUrl: string
  duration: number
  extractor: string
  formats: DownloadFormat[]
  subtitles: { language: string; label: string; automatic: boolean }[]
}

export interface DownloadJob extends DownloadOptions {
  id: string
  url: string
  title: string
  maxHeight: number
  state: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
  message?: string
  fileName?: string
  files?: string[]
  fileSize?: number
  originalSize?: number
  compressed?: boolean
  createdAt: number
  updatedAt: number
}

// Managed tools are downloaded into the app data directory on first use.
export interface ToolStatus {
  name: string
  available: boolean
  version?: string
  managed?: boolean
  installed?: boolean
  downloading?: boolean
  received?: number
  total?: number
  error?: string
}

export interface ToolReport {
  youtubeRecommendation?: { name: string; available: boolean; version?: string }
  tools: ToolStatus[]
  downloadReady: boolean
  downloadDir: string
  cookieConfigured: boolean
}

export interface SubtitleCue {
  start: number
  end: number
  text: string
}

export interface SubtitleTrack {
  language: string
  label: string
  automatic: boolean
  cues: SubtitleCue[]
}

export interface SubtitleLanguage {
  language: string
  label: string
  automatic: boolean
}

export interface SubtitleDocument {
  languages?: SubtitleLanguage[]
  videoId: string
  part: number
  status: 'available' | 'none' | 'login_required'
  tracks: SubtitleTrack[]
}

export interface SavedSubtitle {
  contentId: string
  track: SubtitleTrack
  revision: number
  savedAt: number
}

export interface TextAnchor {
  quote: string
  start: number
  end: number
  prefix: string
  suffix: string
  sourceHash: string
}

export interface ReadingNote {
	 source?: { title: string; author?: string; platform?: string; kind?: string }
	 pinned?: boolean
  tags?: string[]
  id: string
  contentId: string
  body: string
  positionSeconds: number | null
  textAnchor?: TextAnchor | null
  pdfAnchor?: PDFAnchor | null
  subtitleReferences?: SubtitleReference[]
  /** Saved screenshots associated with this note. `frame` is kept for older servers. */
  frames?: NoteFrame[]
  frame?: NoteFrame | null
  createdAt: number
  updatedAt: number
}

export interface SubtitleReference { start: number; end: number; text: string; language: string; trackHash?: string }
export interface SubtitleSourceVersion { language: string; hash: string }
export interface NoteFrame { id: string; downloadId: string; positionSeconds: number; approximate: boolean; width: number; height: number; createdAt: number; source?: 'player' }

export interface PDFRect { x: number; y: number; width: number; height: number }
export interface PDFAnchor { documentId: string; page: number; quote: string; rects: PDFRect[] }

export interface ReadingProgress {
  contentId: string
  positionSeconds: number
  durationSeconds: number
  completed: boolean
  revision: number
  updatedAt: number
}

export interface SubscriptionRefreshState {
  enabled: boolean
  intervalSeconds: number
  lastAttemptAt: number
  lastCheckedAt: number
  nextCheckAt: number
  failures: number
  message?: string
  contentRevision: number
}
export interface SubscriptionSource {
  id: string
  creatorId: string
  name: string
  groupId: string
  url: string
  createdAt: number
  lastUpdatedAt: number
  sourceTitle: string
  itemCount: number
  total: number
  hasMore: boolean
  refresh: SubscriptionRefreshState
  active: boolean
}
export interface SubscriptionGroup { id: string; name: string; createdAt: number; revision: number }
export interface SubscriptionDetail {
  source: SubscriptionSource
  items: ContentItem[]
  notModified?: boolean
}

export interface ReadState { contentId: string; read: boolean; updatedAt: number }

export interface SubscriptionInboxEntry {
  item: ContentItem
  firstSeenAt: number
  lastSeenAt: number
  sources: { id: string; name: string; removed: boolean }[]
}
export interface SubscriptionInboxPage { entries: SubscriptionInboxEntry[]; sources: SubscriptionInboxEntry['sources']; nextCursor: string }
