import { useSubtitleTranslation } from '../features/ai/useSubtitleTranslation'
import { SubtitleTranslation } from '../features/ai/SubtitleTranslation'
import { useSummary } from '../features/ai/useSummary'
import { SummaryControl } from '../features/ai/SummaryControl'
import type { SummarySource } from '../features/ai/summaryMarkdown'
import type { SubtitleTrack } from '../shared/api/types'
import type { SubtitleAssistance } from '../shared/ui/subtitleAssistance'
import { ReaderActions } from '../shared/reader/ReaderActions'
export function useSubtitleAssistance(contentId: string, track: SubtitleTrack | null, onSeek?: (seconds: number) => void, source?: SummarySource, showSummary = true): SubtitleAssistance | undefined {
  const usableTrack = track?.cues.length ? track : null
  const translation = useSubtitleTranslation(contentId, usableTrack ?? undefined)
  const summary = useSummary(contentId, 'subtitle', usableTrack ?? undefined, onSeek)
  return usableTrack ? { track: usableTrack, controls: <><SubtitleTranslation translation={translation} />{showSummary && <ReaderActions><SummaryControl summary={summary} source={source}/></ReaderActions>}</>, texts: translation.show ? translation.inspection?.result.texts : undefined, summary } : undefined
}
