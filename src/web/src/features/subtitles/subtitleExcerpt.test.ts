import { expect, test } from 'vitest'
import type { SubtitleTrack } from '../../shared/api/types'
import { subtitleTrackHash } from '../../shared/media/subtitleSource'
import { subtitleExcerpt } from './subtitleExcerpt'

const original: SubtitleTrack = { language: 'en', label: 'English', automatic: false, cues: Array.from({ length: 220 }, (_, index) => ({ start: index, end: index + 1, text: `Source sentence ${index}.` })) }

test('subtitle versions ignore display labels but distinguish corrected text, timing and language; excerpts use full source boundaries', () => {
  expect(subtitleTrackHash({ ...original, label: '重命名显示' })).toBe(subtitleTrackHash(original))
  for (const changed of [
    { ...original, language: 'fr' },
    { ...original, automatic: true },
    { ...original, cues: [{ ...original.cues[0], text: 'Corrected sentence.' }, ...original.cues.slice(1)] },
    { ...original, cues: [{ ...original.cues[0], end: .9 }, ...original.cues.slice(1)] },
  ]) expect(subtitleTrackHash(changed)).not.toBe(subtitleTrackHash(original))
  const hash = subtitleTrackHash(original)
  expect(subtitleExcerpt(original, [original.cues[0]], 2, hash)).toMatchObject({ start: 0, end: 3, text: 'Source sentence 0. Source sentence 1. Source sentence 2.', trackHash: hash })
  expect(subtitleExcerpt(original, [original.cues[219]], 2, hash)).toMatchObject({ start: 217, end: 220 })
  expect(subtitleExcerpt(original, [original.cues[202], original.cues[204]], 0, hash).text).toBe('Source sentence 202. Source sentence 204.')
  expect(subtitleExcerpt(original, [original.cues[202], original.cues[204]], 1, hash).text).toBe(original.cues.slice(201, 206).map(cue => cue.text).join(' '))
})
