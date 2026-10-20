import type { ReactNode } from 'react'
import type { SubtitleLanguage, SubtitleTrack } from '../api/types'
import type { useSummary } from '../../features/ai/useSummary'
// App composes assistance and subtitles. A track identity prevents old results
// from appearing during a source/language switch before the callback settles.
export interface SubtitleAssistance { track?: SubtitleTrack; controls: ReactNode; texts?: readonly string[]; summary: ReturnType<typeof useSummary>; nativeTarget?: (SubtitleTrack | SubtitleLanguage) & { source?: string }; targetLanguage?: string; onTargetLanguageChange?: (language: string) => void; onPlatformLanguagesChange?: (languages: SubtitleLanguage[], ready: boolean, manualSelection?: boolean) => void }
