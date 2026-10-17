import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SubscriptionGroup } from '../../shared/api/types'

const message = (cause: unknown) => cause instanceof Error ? cause.message : '分组操作未完成，请重试。'

export function useSubscriptionGroups(onMove: (id: string, group: string) => void, onDissolve: (group: string) => void) {
  const [groups, setGroups] = useState<SubscriptionGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState('')
  const [pending, setPending] = useState<{kind: string; id: string} | null>(null)
  const [error, setError] = useState<{kind: string; id: string; message: string} | null>(null)
  const [notice, setNotice] = useState('')
  const listRequest = useRef<AbortController | null>(null)
  const mutation = useRef<AbortController | null>(null)
  const reload = useCallback(async (background = false) => {
    if (mutation.current || background && listRequest.current) return
    listRequest.current?.abort()
    const controller = new AbortController()
    listRequest.current = controller
    if (!background) setLoading(true)
    try {
      const result = await request<{groups: SubscriptionGroup[]}>('/subscription-groups', {signal: controller.signal})
      if (!controller.signal.aborted) { setGroups(result.groups); setListError('') }
    } catch (cause) { if (!controller.signal.aborted) setListError(message(cause)) }
    finally {
      if (listRequest.current === controller) {
        listRequest.current = null
        if (!controller.signal.aborted) setLoading(false)
      }
    }
  }, [])
  useEffect(() => {
    void reload()
    const refresh = () => { if (document.visibilityState === 'visible') void reload(true) }
    const timer = window.setInterval(refresh, 5000)
    window.addEventListener('focus', refresh)
    window.addEventListener('tangentry:subscriptions-changed', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => { window.clearInterval(timer); window.removeEventListener('focus', refresh); window.removeEventListener('tangentry:subscriptions-changed', refresh); document.removeEventListener('visibilitychange', refresh); listRequest.current?.abort(); mutation.current?.abort() }
  }, [reload])

  async function mutate<T>(kind: string, id: string, run: (signal: AbortSignal) => Promise<T>, accept: (result: T) => void) {
    if (mutation.current || loading || listError) return false
    const controller = new AbortController()
    listRequest.current?.abort()
    mutation.current = controller
    setPending({kind, id}); setError(null); setNotice('')
    try {
      const result = await run(controller.signal)
      if (controller.signal.aborted) return false
      accept(result)
      return true
    } catch (cause) { if (!controller.signal.aborted) setError({kind, id, message: message(cause)}); return false }
    finally { if (!controller.signal.aborted) setPending(null); mutation.current = null }
  }
  function replace(group: SubscriptionGroup) {
    setGroups(current => current.some(old => old.id === group.id) ? current.map(old => old.id === group.id ? group : old) : [...current, group])
  }
  const create = (name: string) => mutate('create', '', signal => request<{group: SubscriptionGroup; created: boolean}>('/subscription-groups', {method:'POST', body:JSON.stringify({name}), signal}), result => { replace(result.group); setNotice(result.created ? '分组已创建，可在来源详情中归组。' : '这个分组已存在，保留原有名称和来源。') })
  const rename = (group: SubscriptionGroup, name: string) => mutate('rename', group.id, signal => request<SubscriptionGroup>(`/subscription-groups/${encodeURIComponent(group.id)}`, {method:'PATCH', body:JSON.stringify({name, revision:group.revision}), signal}), result => { replace(result); setNotice('分组名称已保存。') })
  const remove = (group: SubscriptionGroup) => mutate('delete', group.id, signal => request<void>(`/subscription-groups/${encodeURIComponent(group.id)}`, {method:'DELETE', body:JSON.stringify({revision:group.revision}), signal}), () => { setGroups(current => current.filter(old => old.id !== group.id)); onDissolve(group.id); setNotice('分组已解散，来源已回到未分组。') })
  const assign = (id: string, groupId: string, expectedGroupId: string) => mutate('assign', id, signal => request<{sourceId:string; groupId:string}>(`/subscriptions/${encodeURIComponent(id)}/group`, {method:'PUT', body:JSON.stringify({groupId, expectedGroupId}), signal}), result => { onMove(result.sourceId, result.groupId); setNotice('来源分组已保存。') })
  return {groups, loading, listError, error, notice, pending, reload, create, rename, remove, assign}
}
