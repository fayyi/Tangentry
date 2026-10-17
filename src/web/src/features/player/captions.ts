import type { SubtitleCue } from '../../shared/api/types'

function clock(seconds: number): string {
  const total = Math.round(seconds * 1000)
  return `${String(Math.floor(total / 3600000)).padStart(2, '0')}:${String(Math.floor(total / 60000) % 60).padStart(2, '0')}:${String(Math.floor(total / 1000) % 60).padStart(2, '0')}.${String(total % 1000).padStart(3, '0')}`
}

// VTT is generated solely from the already fetched subtitle contract. Escape
// markup so subtitle text cannot become a VTT tag or a second cue/header.
export function toWebVTT(cues: SubtitleCue[]): string {
  return 'WEBVTT\n\n' + cues.map((cue, index) => {
    const text = cue.text.replace(/\r\n?/g, '\n').replace(/\0/g, '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n\s*\n/g, '\n')
    return `${index + 1}\n${clock(cue.start)} --> ${clock(cue.end)}\n${text}\n`
  }).join('\n')
}
