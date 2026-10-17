import type { SubtitleCue } from '../../shared/api/types'

export function activeSubtitleCue(cues: SubtitleCue[], position: number | null) {
  if (position === null || !Number.isFinite(position)) return undefined
  let active: SubtitleCue | undefined
  // Rolling platform captions can overlap; the newest started segment wins.
  for (const cue of cues) {
    if (cue.start <= position && position < cue.end && (!active || cue.start > active.start)) active = cue
  }
  return active
}
