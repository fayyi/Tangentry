import { useEffect, useState } from 'react'
import type { ContentItem, SubtitleCue, SubtitleTrack } from '../../shared/api/types'
import { saveFile } from '../../shared/browser/saveFile'
import { transcriptMarkdown, type TranscriptExportMode } from './transcriptMarkdown'

type SubtitleSource = 'bilibili' | 'youtube' | 'podcast' | 'local'
type SubtitleExportItem = Pick<ContentItem, 'platformId' | 'url'> & Partial<Pick<ContentItem, 'title' | 'author'>>

function srtTimestamp(seconds: number): string {
  const total = Math.round(seconds * 1000)
  const hours = Math.floor(total / 3600000)
  const minutes = Math.floor(total / 60000) % 60
  const secs = Math.floor(total / 1000) % 60
  const milliseconds = total % 1000
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')},${String(milliseconds).padStart(3, '0')}`
}

export function downloadSubtitleSRT(
  videoId: string,
  language: string,
  cues: SubtitleCue[],
  source: SubtitleSource,
  translations?: readonly string[],
  mode: TranscriptExportMode = 'original',
) {
  const text = cues.map((cue, index) => {
    const translation = translations?.[index]?.trim()
    const body = mode === 'translated' && translation
      ? translation
      : mode === 'bilingual' && translation && translation !== cue.text.trim()
        ? `${cue.text}\n${translation}`
        : cue.text
    return `${index + 1}\n${srtTimestamp(cue.start)} --> ${srtTimestamp(cue.end)}\n${body}\n`
  }).join('\n')
  saveFile(
    text,
    `${videoId.replace(':', '-')}${source === 'bilibili' ? '.p1' : ''}.${language}${mode === 'original' ? '' : `.${mode}`}.srt`,
    'application/x-subrip;charset=utf-8',
  )
}

export function useSubtitleExport({
  item,
  source,
  track,
  translated,
  hasTranslations,
  translationsComplete,
}: {
  item: SubtitleExportItem
  source: SubtitleSource
  track?: SubtitleTrack
  translated?: readonly string[]
  hasTranslations: boolean
  translationsComplete: boolean
}) {
  const [exportMode, setExportMode] = useState<TranscriptExportMode>('original')
  const [exportNotice, setExportNotice] = useState('')
  const [exportError, setExportError] = useState('')

  const clearExportFeedback = () => {
    setExportNotice('')
    setExportError('')
  }

  useEffect(() => {
    setExportMode('original')
    clearExportFeedback()
  }, [item.platformId, source, track?.language])

  useEffect(() => {
    if (exportMode !== 'original' && (!hasTranslations || exportMode === 'translated' && !translationsComplete)) {
      setExportMode('original')
    }
  }, [exportMode, hasTranslations, translationsComplete])

  function selectExportMode(mode: TranscriptExportMode) {
    setExportMode(mode)
    clearExportFeedback()
  }

  function exportSRT(mode: TranscriptExportMode = 'original') {
    if (!track) return
    downloadSubtitleSRT(item.platformId, track.language, track.cues, source, translated, mode)
  }

  function exportDocument() {
    if (!track) return
    clearExportFeedback()
    try {
      const contentId = source === 'podcast' || source === 'local' ? item.platformId : `${source}:${item.platformId}`
      const reader = new URL('/reader', window.location.origin)
      reader.searchParams.set('contentId', contentId)
      const text = transcriptMarkdown(
        track,
        { contentId, title: item.title, author: item.author, url: item.url },
        reader.href,
        exportMode,
        translated,
      )
      saveFile(
        text,
        `Tangentry-transcript-${contentId.replace(/[^a-zA-Z0-9_-]/g, '-')}.${track.language.replace(/[^a-zA-Z0-9_-]/g, '-')}.${exportMode}.md`,
        'text/markdown;charset=utf-8',
      )
      setExportNotice('文稿已导出。')
    } catch (cause) {
      setExportError(cause instanceof Error ? cause.message : '文稿导出失败，请重试。')
    }
  }

  return {
    exportMode,
    exportNotice,
    exportError,
    selectExportMode,
    exportSRT,
    exportDocument,
  }
}
