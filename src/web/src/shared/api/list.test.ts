import { describe, expect, test } from 'vitest'
import { listOrEmpty } from './list'

describe('listOrEmpty', () => {
  test('keeps populated and empty lists', () => {
    const values = ['fixture']
    expect(listOrEmpty(values)).toBe(values)
    expect(listOrEmpty([])).toEqual([])
  })

  test('normalizes null and missing API lists', () => {
    expect(listOrEmpty(null)).toEqual([])
    expect(listOrEmpty(undefined)).toEqual([])
    expect(listOrEmpty(null as unknown as string[])).toEqual([])
  })
})
