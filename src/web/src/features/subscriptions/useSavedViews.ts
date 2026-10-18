import { useCallback, useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SavedView, SavedViewDefinition } from '../../shared/api/types'

export function useSavedViews(active: boolean) {
  const [views,setViews] = useState<SavedView[]>([])
  const [loading,setLoading] = useState(false)
  const [error,setError] = useState('')
  const reload = useCallback(async () => {
    setLoading(true);setError('')
    try { const result=await request<{views:SavedView[]}>('/saved-views');setViews(result.views);return result.views }
    catch(cause) { setError(cause instanceof Error?cause.message:'保存视图暂时无法读取。');return null }
    finally { setLoading(false) }
  },[])
  useEffect(()=>{if(active)void reload()},[active,reload])
  const create = async (name:string,definition:SavedViewDefinition,icon='filter',color='teal') => {
    const view=await request<SavedView>('/saved-views',{method:'POST',body:JSON.stringify({name,definition,icon,color})})
    await reload();return view
  }
  const update = async (id:string,edit:Partial<Pick<SavedView,'name'|'definition'|'icon'|'color'|'sortOrder'|'default'>>) => {
    const view=await request<SavedView>(`/saved-views/${encodeURIComponent(id)}`,{method:'PATCH',body:JSON.stringify(edit)})
    await reload();return view
  }
  const remove = async (id:string) => { await request<void>(`/saved-views/${encodeURIComponent(id)}`,{method:'DELETE'});await reload() }
  const reorder = async (ids:string[]) => { await request<void>('/saved-views/order',{method:'PUT',body:JSON.stringify({ids})});await reload() }
  return {views,loading,error,reload,create,update,remove,reorder}
}
