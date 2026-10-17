import { useCallback, useRef } from 'react'
import type { SubtitleCue, SubtitleTrack } from '../shared/api/types'
import type { CapturedFrame, FrameCapture } from '../shared/media/types'
import { subtitleTrackHash } from '../shared/media/subtitleSource'
import { readExcerptRadius, subtitleExcerpt } from '../features/subtitles/subtitleExcerpt'

// The cue on screen, or the one that just ended: a screenshot usually follows
// the sentence that made it worth keeping.
function cueAt(cues: SubtitleCue[], position: number) {
  let current: SubtitleCue | undefined
  for (const cue of cues) {
    if (cue.start > position) break
    if (position < cue.end || position - cue.end <= 4) current = cue
  }
  return current
}

// Composes player capture with the surrounding subtitle lines. Players and the
// notes panel stay unaware of each other; the workspace joins them here.
export function useFrameCapture(capture: () => Promise<CapturedFrame> | undefined, track: SubtitleTrack | null) {
  const currentTrack = useRef(track)
  currentTrack.current = track
  return useCallback(async (): Promise<FrameCapture> => {
    const pending = capture()
    if (!pending) throw new Error('当前播放器不支持截屏。')
    const frame = await pending
    const subtitles = currentTrack.current
    const cue = subtitles ? cueAt(subtitles.cues, frame.positionSeconds) : undefined
    if (!subtitles || !cue) return { frame }
    try {
      return { frame, context: subtitleExcerpt(subtitles, [cue], Math.max(1, readExcerptRadius()) as 1 | 2, subtitleTrackHash(subtitles)) }
    } catch { return { frame } }
  }, [capture])
}
