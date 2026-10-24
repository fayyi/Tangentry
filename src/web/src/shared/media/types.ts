import type { SubtitleReference } from '../api/types'

/** A JPEG still taken from the frame the reader is currently showing. */
export interface CapturedFrame { data: string; width: number; height: number; positionSeconds: number }
/** A capture plus the subtitle lines around it, composed by the reading workspace. */
export interface FrameCapture { frame: CapturedFrame; context?: SubtitleReference }
export interface PlaybackHandle {
  seek: (seconds: number) => boolean
  getPosition: () => number | null
  captureFrame?: () => Promise<CapturedFrame>
  getAudioStream?: () => MediaStream | null
}
