import { describe, expect, it } from 'vitest'
import { selectionActionPosition } from './selectionAction'

describe('selection action position', () => {
  it('keeps the quote action near the selection and inside the viewport', () => {
    expect(selectionActionPosition({ top: 180, bottom: 200, left: 250, right: 450 }, { width: 800, height: 600 })).toEqual({ top: 132, left: 245 })
    expect(selectionActionPosition({ top: 4, bottom: 18, left: 2, right: 12 }, { width: 320, height: 240 })).toEqual({ top: 26, left: 8 })
  })
})
