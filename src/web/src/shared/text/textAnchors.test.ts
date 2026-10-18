import { describe, expect, it } from 'vitest'
import { resolveTextAnchor, translatedTextAnchor } from './textAnchors'

describe('translated text anchors', () => {
  it('maps a Chinese or English selection to the corresponding original passage', () => {
    const text = 'The original sentence.\n\n下一句原文。'
    const anchor = translatedTextAnchor(text, 'a'.repeat(64), '0', '22')
    expect(anchor?.quote).toBe('The original sentence.')
    expect(anchor && resolveTextAnchor(text, 'a'.repeat(64), anchor)).toEqual({ start: 0, end: 22 })
    expect(translatedTextAnchor(text, 'a'.repeat(64), '24', String(text.length))?.quote).toBe('下一句原文。')
  })

  it('rejects invalid translation offsets', () => {
    expect(translatedTextAnchor('source', 'a'.repeat(64), '-1', '3')).toBeNull()
    expect(translatedTextAnchor('source', 'a'.repeat(64), '2', '1')).toBeNull()
    expect(translatedTextAnchor('source', '', '0', '2')).toBeNull()
  })
})
