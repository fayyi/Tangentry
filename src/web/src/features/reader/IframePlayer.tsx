import { forwardRef, useImperativeHandle, useRef } from 'react'
import type { ContentItem } from '../../shared/api/types'
import type { PlaybackHandle } from '../../shared/media/types'
import { YouTubeEmbedPlayer } from '../../shared/media/YouTubeEmbedPlayer'
import { BilibiliEmbedPlayer } from '../../shared/media/BilibiliEmbedPlayer'

interface Props {
  item: ContentItem
  onReady?: (ready: boolean) => void
  onPosition?: (seconds: number | null) => void
  initialTime?: number
}

// Keep the legacy MediaReader path on the same embed implementations used by
// the dedicated video workspaces, including cross-origin screenshot capture.
export const IframePlayer = forwardRef<PlaybackHandle, Props>(function IframePlayer({ item, onReady, onPosition, initialTime = 0 }, ref) {
  const player = useRef<PlaybackHandle>(null)
  useImperativeHandle(ref, () => ({
    getPosition: () => player.current?.getPosition() ?? null,
    seek: seconds => player.current?.seek(seconds) ?? false,
    captureFrame: () => player.current?.captureFrame?.() ?? Promise.reject(new Error('播放器还没有准备好，请稍后再截屏。')),
  }), [])
  if (item.platform === 'youtube') {
    return <YouTubeEmbedPlayer ref={player} id={item.platformId} title={item.title} start={initialTime} autoplay={false} onReady={onReady} onPosition={onPosition}/>
  }
  if (item.platform === 'bilibili') {
    return <BilibiliEmbedPlayer ref={player} id={item.platformId} title={item.title} start={initialTime} autoplay={false} onReady={onReady} onPosition={onPosition}/>
  }
  return <div className="iframe-player-error"><p>当前平台不支持嵌入播放</p></div>
})
