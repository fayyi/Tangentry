import type { ReactNode } from 'react'
import type { SubtitleTrack } from '../api/types'
// App composes assistance and subtitles. A track identity prevents old results
// from appearing during a source/language switch before the callback settles.
export interface SubtitleAssistance { track: SubtitleTrack; controls: ReactNode; texts?: readonly string[] }
