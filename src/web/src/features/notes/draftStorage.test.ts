import { expect, test } from 'vitest'
import { recoverySources } from './draftStorage'

test('draft source discovery reads each candidate once and preserves valid later backups', () => {
  const prefix = 'tangentry.note-recovery.v1.'
  const records: Record<string, string> = {}
  const put = (id: string, owner: string, body: string, updatedAt = 1) => {
    records[prefix + encodeURIComponent(id) + '.' + owner] = JSON.stringify({ raw: JSON.stringify({ body }), updatedAt })
  }
  put('bilibili:BVdraft', 'empty', '')
  records[prefix + encodeURIComponent('bilibili:BVdraft') + '.broken'] = '{'
  put('bilibili:BVdraft', 'valid', '继续编辑')
  put('bilibili:BVdraft', 'other', '另一个窗口')
  put('youtube:video_id', 'valid', '视频想法')
  put('rss:article', 'invalid-time', '不能恢复', NaN)
  put('pdf:document', 'too-long', 'x'.repeat(16001))
  put('unknown:source', 'valid', '不支持的来源')
  records.unrelated = '用户设置'
  let reads = 0
  const storage = Object.assign(Object.create(null), records)
  Object.defineProperty(storage, 'getItem', { value: (key: string) => { reads++; return records[key] ?? null } })
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage })
  try {
    expect(recoverySources()).toEqual(['bilibili:BVdraft', 'youtube:video_id'])
    expect(reads).toBe(6)
  } finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})

test('unavailable draft storage leaves source discovery usable', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, get: () => { throw new Error('storage unavailable') } })
  try { expect(recoverySources()).toEqual([]) }
  finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})
