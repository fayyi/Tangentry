export interface YouTubePlayer {
  getCurrentTime(): number; getDuration(): number; seekTo(seconds: number, allowSeekAhead: boolean): void; destroy(): void
  getPlayerState?(): number
  mute?(): void; unMute?(): void; isMuted?(): boolean
  playVideo?(): void
}
interface YouTubeAPI { Player: new (element: HTMLElement, options: { events: { onReady(): void; onError(): void; onStateChange(event: { data: number }): void; onAutoplayBlocked?(): void } }) => YouTubePlayer }
interface YouTubeWindow extends Window { YT?: YouTubeAPI; onYouTubeIframeAPIReady?: () => void }
let apiLoading: Promise<YouTubeAPI> | null = null

export function loadYouTubeAPI(): Promise<YouTubeAPI> {
  const host = window as YouTubeWindow
  if (host.YT?.Player) return Promise.resolve(host.YT)
  if (apiLoading) return apiLoading
  apiLoading = new Promise<YouTubeAPI>((resolve, reject) => {
    const previous = host.onYouTubeIframeAPIReady
    const script = document.createElement('script')
    let settled = false
    const finish = (error?: Error) => {
      if (settled) return
      settled = true
      clearTimeout(timeout)
      if (host.onYouTubeIframeAPIReady === callback) host.onYouTubeIframeAPIReady = previous
      if (error || !host.YT?.Player) { script.remove(); reject(error ?? new Error('YouTube API unavailable')) }
      else resolve(host.YT)
    }
    const callback = () => { try { previous?.() } finally { finish() } }
    const timeout = window.setTimeout(() => finish(new Error('YouTube API timeout')), 12000)
    host.onYouTubeIframeAPIReady = callback
    script.src = 'https://www.youtube.com/iframe_api'
    script.async = true
    script.onerror = () => finish(new Error('YouTube API unavailable'))
    document.head.append(script)
  }).catch(error => { apiLoading = null; throw error })
  return apiLoading
}
