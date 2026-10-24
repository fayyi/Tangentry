import type { ReadingNote } from '../../shared/api/types'
import { matchesNoteTag } from './tags'
import { formatPosition } from './noteTime'

export type NoteOrder = 'latest' | 'updated' | 'source'

export function readNoteOrder(): NoteOrder {
  try {
    const value = localStorage.getItem('tangentry.note-order.v1')
    if (value === 'updated' || value === 'source') return value
  } catch { /* Optional preference storage must not prevent reading notes. */ }
  return 'latest'
}

function location(note: ReadingNote, unresolvedAnchorIds: string[]): { kind: number; source: string; coordinates: number[] } {
  if (!unresolvedAnchorIds.includes(note.id)) {
    if (note.summaryAnchor) return { kind: 0, source: note.summaryAnchor.nodeId, coordinates: [note.summaryAnchor.level] }
    if (note.textAnchor) return { kind: 0, source: note.textAnchor.sourceHash, coordinates: [note.textAnchor.start, note.textAnchor.end] }
    if (note.pdfAnchor) {
      const first = [...note.pdfAnchor.rects].sort((a, b) => a.y - b.y || a.x - b.x)[0]
      return { kind: 1, source: note.pdfAnchor.documentId, coordinates: [note.pdfAnchor.page, first?.y ?? 0, first?.x ?? 0] }
    }
  }
  if (!note.textAnchor && !note.pdfAnchor && note.positionSeconds !== null) return { kind: 2, source: '', coordinates: [note.positionSeconds] }
  if (note.subtitleReferences?.length) return { kind: 2, source: '', coordinates: [note.subtitleReferences[0].start] }
  return { kind: 3, source: '', coordinates: [] }
}

export function linkedNote(note: ReadingNote): boolean {
  return Boolean(note.summaryAnchor || note.textAnchor || note.pdfAnchor || note.subtitleReferences?.length || note.positionSeconds !== null)
}

export function filterNotes(notes: ReadingNote[], query: string, filter: 'all' | 'linked' | 'free', tag: string, order: NoteOrder, unresolvedAnchorIds: string[]): ReadingNote[] {
  const search = query.trim().toLocaleLowerCase()
  const result = notes.filter(note => {
    const linked = linkedNote(note)
    const searchable = [note.body, note.summaryAnchor?.quote, note.summaryAnchor?.shown, note.textAnchor?.quote, note.pdfAnchor?.quote, ...(note.subtitleReferences ?? []).map(reference => reference.text), ...(note.tags ?? []), note.positionSeconds === null ? '' : formatPosition(note.positionSeconds)]
    return matchesNoteTag(note.tags, tag) && (filter === 'all' || (filter === 'linked' ? linked : !linked)) && (!search || searchable.filter(Boolean).join('\n').toLocaleLowerCase().includes(search))
  })
  if (order === 'latest') return result
  if (order === 'updated') return result.sort((a, b) => b.updatedAt - a.updatedAt)
  const locations = new Map(result.map(note => [note.id, location(note, unresolvedAnchorIds)]))
  return result.sort((a, b) => {
    const left = locations.get(a.id)!, right = locations.get(b.id)!
    const group = left.kind - right.kind || left.source.localeCompare(right.source)
    if (group) return group
    for (let index = 0; index < left.coordinates.length; index++) {
      const difference = left.coordinates[index] - right.coordinates[index]
      if (difference) return difference
    }
    return 0
  })
}
