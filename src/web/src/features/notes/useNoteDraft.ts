import { useEffect, useMemo, useRef, useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { emptyDraft, readDraft, type Draft } from './noteDraft'
import {
  deleteRecoveryMessage,
  persistRecovery,
  readRecoveryOrigin,
  recoveryDrafts,
  recoveryKey,
  rememberRecoveryOrigin,
  removeRecovery,
  type RecoveryDraft,
} from './draftStorage'

type DraftSetter = Dispatch<SetStateAction<Draft>>

export function useNoteDraft(contentId: string) {
  const draftKey = `tangentry.note-draft.v1.${contentId}`
  const backupKey = recoveryKey(contentId)
  const [draft, setDraft] = useState<Draft>(() => readDraft(draftKey))
  const currentDraft = useRef(draft)
  currentDraft.current = draft
  const [backups, setBackups] = useState(() => recoveryDrafts(contentId))
  const [backupFailed, setBackupFailed] = useState(false)
  const [backupNotice, setBackupNotice] = useState('')
  const recovered = useRef<RecoveryDraft | null>(readRecoveryOrigin(contentId))

  const recoverable = useMemo(() => backups.filter(backup => {
    if (backup.key === backupKey || backup.raw === JSON.stringify(draft)) return false
    const value = readDraft('', backup.raw)
    return Boolean(value.body || value.tags || value.textAnchor || value.pdfAnchor || value.subtitleReferences?.length || value.images?.length || value.editingId || value.positionSeconds !== null)
  }), [backups, backupKey, draft])

  useEffect(() => {
    function refresh() { setBackups(recoveryDrafts(contentId)) }
    window.addEventListener('storage', refresh)
    return () => window.removeEventListener('storage', refresh)
  }, [contentId])

  useEffect(() => {
    try {
      if (draft.body || draft.tags || draft.positionSeconds !== null || draft.editingId || draft.textAnchor || draft.pdfAnchor || draft.subtitleReferences?.length || draft.images?.length) {
        sessionStorage.setItem(draftKey, JSON.stringify(draft))
      } else {
        sessionStorage.removeItem(draftKey)
      }
    } catch { /* Full or disabled storage must not prevent a manual save. */ }
    // JPEG payloads stay in this window's session draft; recovery storage keeps text and identity only.
    setBackupFailed(!persistRecovery(backupKey, draftHasContent(draft) ? JSON.stringify({ ...draft, image: undefined, images: undefined }) : null))
  }, [draftKey, backupKey, draft])

  function clearDraft() {
    if (recovered.current) removeRecovery(recovered.current)
    recovered.current = null
    rememberRecoveryOrigin(contentId, null)
    setDraft({ ...emptyDraft })
    setBackups(recoveryDrafts(contentId))
  }

  function deleteBackup(backup: RecoveryDraft) {
    setBackupNotice(deleteRecoveryMessage(backup))
    setBackups(recoveryDrafts(contentId))
  }

  function restoreDraft(backup: RecoveryDraft) {
    const value = readDraft('', backup.raw)
    recovered.current = backup
    rememberRecoveryOrigin(contentId, backup)
    setDraft(value)
    return value
  }

  function persistRetry(draftValue: Draft) {
    currentDraft.current = draftValue
    setDraft(draftValue)
    try { sessionStorage.setItem(draftKey, JSON.stringify(draftValue)) } catch { /* In-memory state still retains the retry identity. */ }
    setBackupFailed(!persistRecovery(backupKey, JSON.stringify({ ...draftValue, image: undefined, images: undefined })))
  }

  return {
    draft,
    setDraft: setDraft as DraftSetter,
    currentDraft,
    draftKey,
    backupKey,
    backups,
    recoverable,
    backupFailed,
    backupNotice,
    recovered,
    clearDraft,
    deleteBackup,
    restoreDraft,
    persistRetry,
  }
}

function draftHasContent(draft: Draft): boolean {
  return Boolean(draft.body || draft.tags || draft.positionSeconds !== null || draft.editingId || draft.textAnchor || draft.pdfAnchor || draft.subtitleReferences?.length || draft.images?.length)
}