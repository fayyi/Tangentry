function editorWindowId(): string {
  const fresh = crypto.randomUUID()
  try {
    const key = 'tangentry.editor-window'
    const previous = sessionStorage.getItem(key)
    const navigation = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
    // A newly opened tab can inherit its opener's session storage, but not its editor identity.
    const id = navigation?.type === 'reload' && previous && /^[a-f0-9-]{36}$/.test(previous) ? previous : fresh
    sessionStorage.setItem(key, id)
    return id
  } catch { return fresh }
}

const windowId = editorWindowId()
const prefix = 'tangentry.note-recovery.v1.'

export interface RecoveryDraft { key: string; raw: string; updatedAt: number }

export function recoveryKey(contentId: string): string {
  return prefix + encodeURIComponent(contentId) + '.' + windowId
}

export function readRecoveryOrigin(contentId: string): RecoveryDraft | null {
  try {
    const value = JSON.parse(sessionStorage.getItem('tangentry.recovery-origin.' + contentId) ?? 'null')
    const snapshot = value?.snapshot
    return value?.owner === recoveryKey(contentId) && snapshot &&
      typeof snapshot.key === 'string' && snapshot.key.startsWith(prefix + encodeURIComponent(contentId) + '.') &&
      typeof snapshot.raw === 'string' && snapshot.raw.length <= 128000 && Number.isFinite(snapshot.updatedAt)
      ? snapshot : null
  } catch { return null }
}

export function rememberRecoveryOrigin(contentId: string, snapshot: RecoveryDraft | null) {
  try {
    const key = 'tangentry.recovery-origin.' + contentId
    if (snapshot) sessionStorage.setItem(key, JSON.stringify({ owner: recoveryKey(contentId), snapshot }))
    else sessionStorage.removeItem(key)
  } catch { /* Failure only retains an extra backup; it cannot discard another editor's draft. */ }
}

export function recoveryDrafts(contentId: string): RecoveryDraft[] {
  try {
    const start = prefix + encodeURIComponent(contentId) + '.'
    return Object.keys(localStorage).filter(key => key.startsWith(start)).flatMap(key => {
      try {
        const value = JSON.parse(localStorage.getItem(key) ?? 'null')
        return value && typeof value.raw === 'string' && value.raw.length <= 128000 && Number.isFinite(value.updatedAt)
          ? [{ key, raw: value.raw, updatedAt: value.updatedAt }] : []
      } catch { return [] }
    }).sort((a, b) => b.updatedAt - a.updatedAt)
  } catch { return [] }
}

export function recoverySources(): string[] {
  try {
    const sources = new Set<string>()
    for (const key of Object.keys(localStorage)) {
      if (!key.startsWith(prefix)) continue
      const encoded = key.slice(prefix.length, key.lastIndexOf('.'))
      try {
        const id = decodeURIComponent(encoded)
        if (!/^(?:bilibili|youtube|douyin|xiaohongshu|xiaoyuzhou|rss|pdf|memo):[A-Za-z0-9_-]+$/.test(id)) continue
        if (sources.has(id)) continue
        const backup = JSON.parse(localStorage.getItem(key) ?? 'null')
        if (!backup || typeof backup.raw !== 'string' || backup.raw.length > 128000 || !Number.isFinite(backup.updatedAt)) continue
        const draft = JSON.parse(backup.raw)
        if (typeof draft?.body === 'string' && draft.body.length <= 16000 &&
          Boolean(draft.body || draft.tags || draft.textAnchor || draft.pdfAnchor || draft.subtitleReferences?.length || draft.editingId || draft.positionSeconds != null)) sources.add(id)
      } catch { /* Ignore malformed backup keys. */ }
    }
    return [...sources]
  } catch { return [] }
}

export function persistRecovery(key: string, raw: string | null): boolean {
  try {
    const previous = localStorage.getItem(key)
    if (raw === null && previous === null) return true
    if (raw !== null && previous) {
      try { if (JSON.parse(previous).raw === raw) return true }
      catch { /* Replace malformed records belonging to this editor. */ }
    }
    if (raw === null) localStorage.removeItem(key)
    else localStorage.setItem(key, JSON.stringify({ raw, updatedAt: Date.now() }))
    window.dispatchEvent(new Event('tangentry-drafts-changed'))
    return true
  } catch { return false }
}

export function removeRecovery(snapshot: RecoveryDraft): 'removed' | 'changed' | 'unavailable' {
  try {
    const current = JSON.parse(localStorage.getItem(snapshot.key) ?? 'null')
    if (current === null) return 'removed'
    // An active editor may have written a newer version since recovery was offered.
    if (current?.raw === snapshot.raw && current?.updatedAt === snapshot.updatedAt) {
      localStorage.removeItem(snapshot.key)
      window.dispatchEvent(new Event('tangentry-drafts-changed'))
      return 'removed'
    }
    return 'changed'
  } catch { return 'unavailable' }
}

export function deleteRecoveryMessage(snapshot: RecoveryDraft): string {
  const result = removeRecovery(snapshot)
  return result === 'removed' ? '备份草稿已删除。' : result === 'changed'
    ? '备份已在其他窗口更新，请确认最新内容后再删除。'
    : '无法删除浏览器中的备份草稿，请检查浏览器存储权限后重试。'
}
