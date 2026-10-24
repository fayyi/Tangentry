import type { SubtitleAssistance } from '../../shared/ui/subtitleAssistance'
import type { ContentItem, SubtitleTrack, SubtitleReference } from '../../shared/api/types'
import { SubtitleReader } from './SubtitleReader'

export function PodcastTranscript({ item, position, onSeek, onNoteAt, onTrackChange, assistance, audioStream }: { assistance?: SubtitleAssistance; onTrackChange?: (track: SubtitleTrack | null) => void; item: ContentItem; position: number | null; onSeek?: (seconds: number) => void; onNoteAt: (seconds: number, reference?: SubtitleReference) => void; audioStream?: MediaStream | null }) {
  return <section className="podcast-transcript" aria-label={item.kind==='video'?'视频字幕':'播客文字稿'}><SubtitleReader source={(item.platform==='douyin'||item.platform==='xiaohongshu')?'local':'podcast'} item={{ ...item, platformId: item.id }} position={position} onSeek={onSeek} onNoteAt={onNoteAt} onTrackChange={onTrackChange} assistance={assistance} audioStream={audioStream}/></section>
}
