import { useCallback, useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import type { SubscriptionInboxPage } from '../../shared/api/types'
import { useInboxPageSize } from '../settings/useInboxPageSize'

export interface InboxFilters { query: string; sourceId: string; groupId: string; kind: string; read: string }
const emptyFilters: InboxFilters = {query:'',sourceId:'',groupId:'',kind:'',read:'all'}
const emptyPage: SubscriptionInboxPage = {entries:[],sources:[],nextCursor:''}

// Lives with App so filters, pagination and reader return survive page unmounts.
// Pages are explicit: at most 50 displayed IDs can enter a bulk read action.
export function useSubscriptionInbox(active: boolean) {
  const { pageSize } = useInboxPageSize()
  const loadedPageSize = useRef(pageSize)
  const [filters,setFilters] = useState<InboxFilters>(emptyFilters)
  const [page,setPage] = useState(emptyPage)
  const [previous,setPrevious] = useState<SubscriptionInboxPage[]>([])
  const [loading,setLoading] = useState(false), [error,setError] = useState('')
  const [confirmed,setConfirmed] = useState(false)
  const controller = useRef<AbortController|null>(null)
  const confirmedFilters = useRef(emptyFilters)
  const generation = useRef(0)
  const load = useCallback(async (nextFilters: InboxFilters, cursor='',append=false) => {
    controller.current?.abort()
    const current = new AbortController(), token=++generation.current
    controller.current=current
    setLoading(true);setError('')
    const params=new URLSearchParams({...nextFilters,q:nextFilters.query,cursor,limit:String(pageSize)})
    params.delete('query')
    try {
      const result=await request<SubscriptionInboxPage>('/subscription-inbox?'+params,{signal:current.signal})
      if(current.signal.aborted || token!==generation.current)return false
      setPrevious(old=>append?[...old,page]:[])
      setPage(result);setFilters(nextFilters);confirmedFilters.current=nextFilters;setConfirmed(true)
      return true
    } catch(cause) {
      if(!current.signal.aborted && token===generation.current)setError(cause instanceof Error?cause.message:'收件箱暂时无法读取。')
      return false
    } finally { if(!current.signal.aborted && token===generation.current)setLoading(false) }
  },[page,pageSize])
  useEffect(() => {
    if (loadedPageSize.current === pageSize) return
    loadedPageSize.current = pageSize
    controller.current?.abort(); ++generation.current
    setPage(emptyPage); setPrevious([]); setConfirmed(false); setLoading(false); setError('')
  }, [pageSize])
  useEffect(()=>{if(active&&!confirmed&&!loading&&!error)void load(confirmedFilters.current)},[active,confirmed,loading,error,load])
  useEffect(()=>()=>{controller.current?.abort()},[])
  const back=()=>{if(loading||!previous.length)return;controller.current?.abort();++generation.current;setError('');setPage(previous[previous.length-1]);setPrevious(previous.slice(0,-1))}
  return {filters,page,loading,error,confirmed,pageNumber:previous.length+1,hasPrevious:previous.length>0,apply:(next:InboxFilters)=>load(next),reload:()=>load(confirmedFilters.current),next:()=>page.nextCursor?load(confirmedFilters.current,page.nextCursor,true):Promise.resolve(false),back}
}
