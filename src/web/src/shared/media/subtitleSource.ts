import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex } from '@noble/hashes/utils.js'
import type { SubtitleTrack } from '../api/types'

export function subtitleTrackHash(track: SubtitleTrack): string {
  const source = JSON.stringify({
    language: track.language, automatic: track.automatic,
    cues: track.cues.map(cue => [cue.start, cue.end, cue.text]),
  })
  return bytesToHex(sha256(new TextEncoder().encode('tangentry.subtitle.v1\n' + source)))
}
