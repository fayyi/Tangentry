import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SavedViewFilters, SavedViewInboxPage, SubscriptionInboxPage } from '../../shared/api/types'
import { useInboxPageSize } from '../settings/useInboxPageSize'
import { emptySavedViewFilters, relativeDateFilterTimestamp } from './savedViewDefinition'

export type InboxFilters = SavedViewFilters
export const emptyFilters: InboxFilters = emptySavedViewFilters
const emptyPage: SubscriptionInboxPage = {entries:[],sources:[],nextCursor:''}

// Lives with App so filters, pagination and reader return survive page unmounts.
// Pages are explicit: at most 50 displayed IDs can enter a bulk read action.
export function useSubscriptionInbox(active: boolean, savedViewID = '') {
  const { pageSize } = useInboxPageSize()
  const loadedPageSize = useRef(pageSize)
  const [filters,setFilters] = useState<InboxFilters>(emptyFilters)
  const [page,setPage] = useState(emptyPage)
  const [count,setCount] = useState(0)
  const [previous,setPrevious] = useState<SubscriptionInboxPage[]>([])
  const [loading,setLoading] = useState(false), [error,setError] = useState('')
  const [confirmed,setConfirmed] = useState(false)
  const controller = useRef<AbortController|null>(null)
  const confirmedFilters = useRef(emptyFilters)
  const generation = useRef(0)
  const activeSavedViewID=savedViewID
  const skipViewReset=useRef(false)
  const loadedViewID=useRef(activeSavedViewID)
  const load = useCallback(async (nextFilters: InboxFilters, cursor='',append=false,savedViewOverride?:string) => {
    controller.current?.abort()
    const current = new AbortController(), token=++generation.current
    controller.current=current
    setLoading(true);setError('')
    const viewID=savedViewOverride===undefined?activeSavedViewID:savedViewOverride
    const params=viewID?new URLSearchParams({cursor,limit:String(pageSize)}):new URLSearchParams({q:nextFilters.query,sourceId:nextFilters.sourceId,groupId:nextFilters.groupId,platform:nextFilters.platform,kind:nextFilters.kind,topic:nextFilters.topic,tag:nextFilters.tag,read:nextFilters.read,favorite:String(nextFilters.favorite),since:nextFilters.since?String(nextFilters.since):String(relativeDateFilterTimestamp(nextFilters.sinceOffset)),until:nextFilters.until?String(nextFilters.until):'',cursor,limit:String(pageSize)})
    for (const [key,value] of params) if (!value) params.delete(key)
    try {
      const path=viewID?`/saved-views/${encodeURIComponent(viewID)}/inbox?${params}`:`/subscription-inbox?${params}`
      const response=await request<SubscriptionInboxPage|SavedViewInboxPage>(path,{signal:current.signal})
      if(current.signal.aborted || token!==generation.current)return false
      const result='page' in response?response.page:response
      setPrevious(old=>append?[...old,page]:[])
      setPage(result);setFilters(nextFilters);confirmedFilters.current=nextFilters;setConfirmed(true)
      setCount('page' in response?response.count:result.entries.length)
      return true
    } catch(cause) {
      if(!current.signal.aborted && token===generation.current)setError(cause instanceof Error?cause.message:'收件箱暂时无法读取。')
      return false
    } finally { if(!current.signal.aborted && token===generation.current)setLoading(false) }
  },[activeSavedViewID,page,pageSize])
  const reset = useCallback(() => {
    controller.current?.abort();++generation.current
    setPage(emptyPage);setPrevious([]);setConfirmed(false);setLoading(false);setError('')
  },[])
  useEffect(()=>{
    if(loadedViewID.current===activeSavedViewID)return
    loadedViewID.current=activeSavedViewID
    if(skipViewReset.current){skipViewReset.current=false;return}
    reset()
  },[activeSavedViewID,reset])
  useEffect(() => {
    if (loadedPageSize.current === pageSize) return
    loadedPageSize.current = pageSize
    controller.current?.abort(); ++generation.current
    setPage(emptyPage); setPrevious([]); setConfirmed(false); setLoading(false); setError('')
  }, [pageSize])
  useEffect(()=>{if(active&&!confirmed&&!loading&&!error)void load(confirmedFilters.current)},[active,confirmed,loading,error,load])
  useEffect(()=>()=>{controller.current?.abort()},[])
  const back=()=>{if(loading||!previous.length)return;controller.current?.abort();++generation.current;setError('');setPage(previous[previous.length-1]);setPrevious(previous.slice(0,-1))}
  const apply=(next:InboxFilters)=>{
    setFilters(next)
    confirmedFilters.current=next
    if(savedViewID)skipViewReset.current=true
    return load(next,'',false,'')
  }
  return {
    filters,page,count,loading,error,confirmed,pageNumber:previous.length+1,hasPrevious:previous.length>0,
    apply,
    selectSavedView:reset,
    updateSavedView:(next:InboxFilters)=>load(next),reload:()=>load(confirmedFilters.current),
    next:()=>page.nextCursor?load(confirmedFilters.current,page.nextCursor,true):Promise.resolve(false),back,
  }
}
