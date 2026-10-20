import { useState } from 'react'
import { useSubtitleTranslation } from '../features/ai/useSubtitleTranslation'
import { SubtitleTranslation } from '../features/ai/SubtitleTranslation'
import { useSummary } from '../features/ai/useSummary'
import { SummaryControl } from '../features/ai/SummaryControl'
import type { SummarySource } from '../features/ai/summaryMarkdown'
import type { SubtitleLanguage, SubtitleTrack } from '../shared/api/types'
import { preferredSubtitleLanguage, subtitleTargetLabel } from '../features/subtitles/subtitleLanguageMatch'
import type { SubtitleAssistance } from '../shared/ui/subtitleAssistance'
import { ReaderActions } from '../shared/reader/ReaderActions'
export function useSubtitleAssistance(contentId: string, track: SubtitleTrack | null, onSeek?: (seconds: number) => void, source?: SummarySource, showSummary = true): SubtitleAssistance | undefined {
  const [availableLanguages, setAvailableLanguages] = useState<SubtitleLanguage[]>([])
  const [selectedTarget, setSelectedTarget] = useState('zh-Hans')
  const usableTrack = track?.cues.length ? track : null
  const nativeTarget = preferredSubtitleLanguage(availableLanguages, selectedTarget)
  const translation = useSubtitleTranslation(contentId, usableTrack ?? undefined, undefined, selectedTarget, availableLanguages)
  const summary = useSummary(contentId, 'subtitle', usableTrack ?? undefined, onSeek)
  if (!usableTrack && !nativeTarget && availableLanguages.length === 0) return undefined
  return { track: usableTrack ?? undefined, nativeTarget, targetLanguage: selectedTarget, onTargetLanguageChange: setSelectedTarget, onPlatformLanguagesChange: setAvailableLanguages, controls: <><SubtitleTranslation translation={translation} targetLabel={subtitleTargetLabel(selectedTarget)} nativeTarget={nativeTarget} targetLanguage={selectedTarget} onTargetLanguageChange={setSelectedTarget} hasSourceTrack={Boolean(usableTrack)} />{usableTrack && showSummary && <ReaderActions><SummaryControl summary={summary} source={source}/></ReaderActions>}</>, texts: translation.show ? translation.inspection?.result.texts : undefined, summary }
}
