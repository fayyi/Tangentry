import { describe, expect, it } from 'vitest'
import { availableSubtitleLanguages, resolveNativeSubtitleTarget } from './nativeSubtitleTarget'

const japanese = { language: 'ja', label: '日语', automatic: false, source: 'cc' as const, hasUrl: true }
const source = { language: 'en', label: '英语', automatic: false, cues: [{ start: 0, end: 1, text: 'Source cue' }] }
const japaneseTrack = { language: 'ja', label: '日语', automatic: false, source: 'cc' as const, cues: [{ start: 10, end: 11, text: 'Native cue' }] }

describe('native subtitle target reads', () => {
  it('keeps a source fallback and reports loading until target cues arrive', () => {
    const result = resolveNativeSubtitleTarget(japanese, [source], { language: 'ja', status: 'loading' }, source)
    expect(result).toMatchObject({ track: undefined, displayTrack: source, status: 'loading', failed: false })
  })

  it('selects the native target track as soon as its own cues are available', () => {
    const result = resolveNativeSubtitleTarget(japanese, [source, japaneseTrack], { language: 'ja', status: 'loaded' })
    expect(result).toMatchObject({ track: japaneseTrack, status: 'loaded', failed: false })
  })

  it('marks failed target metadata unavailable so the source can use AI fallback', () => {
    const result = resolveNativeSubtitleTarget(japanese, [source], { language: 'ja', status: 'error', error: 'fixture failure' }, source)
    expect(result).toMatchObject({ track: undefined, displayTrack: source, status: 'error', failed: true })
    expect(availableSubtitleLanguages([japanese, { ...japanese, language: 'en' }], 'ja').map(value => value.language)).toEqual(['en'])
  })
})
