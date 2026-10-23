import { Button, Checkbox, Input, Select } from '../../components/ui'
import { Check, CheckCheck, Circle, ChevronDown, ChevronLeft, ChevronRight, RefreshCw, Search, Save, Trash2, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { ContentItem, SubscriptionGroup, SubscriptionSource } from '../../shared/api/types'
import { ContentCard } from '../../shared/ui/ContentCard'
import { CardCollection } from '../../shared/ui/CardCollection'
import type { useReadState } from '../readstate/useReadState'
import type { useSubscriptionInbox } from './useSubscriptionInbox'
import type { SavedView } from '../../shared/api/types'
import { dateFilterTimestamp, localDateInputValue } from './savedViewDefinition'

type Inbox = ReturnType<typeof useSubscriptionInbox>
function InboxOptions({label,name,value,options,onChange}:{label:string;name:string;value:string;options:[string,string][];onChange:(value:string)=>void}) {
  return <div className="inbox-options" role="radiogroup" aria-label={label}>{options.map(([id,text])=><label key={id}><Input type="radio" name={name} value={id} checked={value===id} onChange={()=>onChange(id)}/><span>{text}</span></label>)}</div>
}
export function SubscriptionInbox({inbox,sources,groups,readStates,savedIDs,savingIDs,savingUnavailable,onRead,onSave,saveNotice,saveError,views,selectedViewID,onSelectView,onApplyFilters,onSaveView,onUpdateView,onDeleteView}:{inbox:Inbox;sources:SubscriptionSource[];groups:SubscriptionGroup[];readStates:ReturnType<typeof useReadState>;savedIDs:Set<string>;savingIDs:Set<string>;savingUnavailable:boolean;onRead:(item:ContentItem)=>void;onSave:(item:ContentItem)=>void;saveNotice:string;saveError:boolean;views:SavedView[];selectedViewID:string;onSelectView:(id:string)=>void;onApplyFilters:(filters:Inbox['filters'])=>void;onSaveView:(name:string,filters:Inbox['filters'])=>Promise<void>;onUpdateView:(id:string,edit:Partial<Pick<SavedView,'name'|'definition'|'icon'|'color'|'default'>>)=>void;onDeleteView:(id:string)=>void}){
  const [draft,setDraft]=useState(inbox.filters)
  const [namingView,setNamingView]=useState(false)
  const [viewName,setViewName]=useState('')
  const [saveViewError,setSaveViewError]=useState('')
  useEffect(()=>{
    const active=views.find(view=>view.id===selectedViewID)
    if(active) setDraft(active.definition.filters)
  },[selectedViewID,views])
  const busy=inbox.loading||!inbox.confirmed||!readStates.ready||Boolean(readStates.pending.size)
  const sourceOptions=[...sources.map(source=>({id:source.id,name:source.name})),...inbox.page.sources.filter(source=>source.removed && !sources.some(value=>value.id===source.id))].filter((source,index,list)=>list.findIndex(value=>value.id===source.id)===index)
  const builtInViews=views.filter(view=>view.builtin)
  return <section className="subscription-inbox" aria-label="历史收件箱">
    <h2 className="visually-hidden">历史收件箱</h2>
    {builtInViews.length>0&&<nav className="inbox-presets" aria-label="常用订阅筛选"><span>常用筛选</span>{builtInViews.map(view=><Button variant="quiet" type="button" key={view.id} aria-pressed={selectedViewID===view.id} onClick={()=>onSelectView(view.id)}>{view.name}<small>{view.count}</small></Button>)}</nav>}
    <section className="inbox-filters" aria-label="收件箱筛选">
      <div className="inbox-search-row">
        <label className="subscription-field inbox-search"><span className="visually-hidden">搜索历史内容</span><div className="subscription-input"><Search size={16} aria-hidden="true"/><Input type="search" value={draft.query} maxLength={400} placeholder="搜索标题或作者" onChange={event=>setDraft({...draft,query:event.target.value})}/></div></label>
        <Button variant="primary" className="button button-primary" disabled={inbox.loading} onClick={()=>onApplyFilters(draft)}>应用筛选</Button>
        <Button variant="quiet" iconOnly type="button" className="icon-button" aria-label="重新读取收件箱" title="重新读取收件箱" disabled={inbox.loading} onClick={()=>void inbox.reload()}><RefreshCw size={16} className={inbox.loading?'spin':undefined}/></Button>
      </div>
      <div className="inbox-filter-strip inbox-primary-filters">
        <label className="subscription-field"><span className="visually-hidden">来源</span><Select value={draft.sourceId} onChange={event=>setDraft({...draft,sourceId:event.target.value})}><option value="">全部来源</option>{sourceOptions.map(source=><option key={source.id} value={source.id}>{source.name}{sources.some(value=>value.id===source.id)?'':'（已移除）'}</option>)}{draft.sourceId&&!sourceOptions.some(source=>source.id===draft.sourceId)&&<option value={draft.sourceId}>已移除的来源</option>}</Select></label>
        <InboxOptions label="内容类型" name="inbox-kind" value={draft.kind} options={[["","全部类型"],["graphical","图文"],["audio","音频"],["video","视频"],["pdf","PDF"]]} onChange={kind=>setDraft({...draft,kind})}/>
        <InboxOptions label="阅读状态" name="inbox-read" value={draft.read} options={[["all","全部状态"],["unread","未读"],["read","已读"]]} onChange={read=>setDraft({...draft,read})}/>
        <details className="inbox-advanced"><summary>更多条件<ChevronDown size={14} aria-hidden="true"/></summary><div className="inbox-advanced-fields">
          <label className="subscription-field"><span>分组</span><Select value={draft.groupId} onChange={event=>setDraft({...draft,groupId:event.target.value})}><option value="">全部分组</option>{groups.map(group=><option key={group.id} value={group.id}>{group.name}</option>)}{draft.groupId&&!groups.some(group=>group.id===draft.groupId)&&<option value={draft.groupId}>已解散的分组</option>}</Select></label>
          <label className="subscription-field"><span>平台</span><Select value={draft.platform} onChange={event=>setDraft({...draft,platform:event.target.value})}><option value="">全部平台</option>{[...new Set(sources.map(source=>source.id.split(':')[0]))].map(platform=><option key={platform} value={platform}>{platform}</option>)}</Select></label>
          <label className="subscription-field"><span>主题 / 分类</span><Input value={draft.topic} maxLength={80} onChange={event=>setDraft({...draft,topic:event.target.value})}/></label>
          <label className="subscription-field"><span>笔记标签</span><Input value={draft.tag} maxLength={80} onChange={event=>setDraft({...draft,tag:event.target.value})}/></label>
          <label className="subscription-field"><span>时间范围</span><Select value={draft.sinceOffset} onChange={event=>setDraft({...draft,sinceOffset:event.target.value,since:0,until:0})}><option value="">不限</option><option value="day">今天</option><option value="week">本周</option><option value="month">本月</option></Select></label>
          <label className="subscription-field"><span>开始日期</span><Input type="date" value={localDateInputValue(draft.since)} onChange={event=>setDraft({...draft,sinceOffset:'',since:dateFilterTimestamp(event.target.value),until:draft.until})}/></label>
          <label className="subscription-field"><span>结束日期</span><Input type="date" value={localDateInputValue(draft.until)} onChange={event=>setDraft({...draft,sinceOffset:'',since:draft.since,until:dateFilterTimestamp(event.target.value,true)})}/></label>
          <label className="inbox-checkbox"><Checkbox type="checkbox" checked={draft.favorite} onChange={event=>setDraft({...draft,favorite:event.target.checked})}/>仅收藏</label>
        </div></details>
      </div>
      <div className="inbox-view-actions">{namingView?<div className="inbox-view-name-form"><label><span className="visually-hidden">视图名称</span><Input autoFocus aria-label="视图名称" value={viewName} maxLength={40} placeholder="输入视图名称" required onChange={event=>setViewName(event.target.value)}/></label><Button variant="primary" className="button button-primary" type="button" disabled={!viewName.trim()} onClick={()=>{const name=viewName.trim();if(!name)return;setSaveViewError('');void onSaveView(name,draft).then(()=>setNamingView(false)).catch(cause=>setSaveViewError(cause instanceof Error?cause.message:'视图暂时无法保存。'))}}><Save size={15}/>保存视图</Button><Button variant="quiet" iconOnly className="icon-button" type="button" aria-label="取消保存视图" title="取消" onClick={()=>{setNamingView(false);setSaveViewError('')}}><X size={16}/></Button></div>:<Button variant="secondary" className="button button-secondary" type="button" onClick={()=>{const existing=views.find(view=>view.id===selectedViewID);setViewName(existing?.builtin?`${existing.name}（自定义）`:existing?.name??'');setNamingView(true)}}><Save size={15}/>{views.find(view=>view.id===selectedViewID)?.builtin?'另存为自定义视图':selectedViewID?'保存视图':'保存为视图'}</Button>}<span>{selectedViewID?`当前视图：${views.find(view=>view.id===selectedViewID)?.name??''}`:'筛选只会在点击“应用筛选”后生效'}</span>{saveViewError&&<small role="alert">{saveViewError}</small>}</div>
      {views.find(view=>view.id===selectedViewID&&!view.builtin)&&(()=>{const active=views.find(view=>view.id===selectedViewID&&!view.builtin)!;return <div className="saved-view-settings" aria-label="视图设置">
        <label className="subscription-field"><span>名称</span><Input value={active.name} maxLength={40} onChange={event=>{if(event.target.value.trim())onUpdateView(active.id,{name:event.target.value})}} onBlur={event=>{if(event.target.value!==active.name)onUpdateView(active.id,{name:event.target.value.trim()})}}/></label>
        <label className="subscription-field"><span>图标</span><Select value={active.icon} onChange={event=>onUpdateView(active.id,{icon:event.target.value})}><option value="filter">筛选</option><option value="file-text">文章</option><option value="play">视频</option><option value="headphones">播客</option><option value="image">图片</option><option value="bookmark">收藏</option><option value="newspaper">资讯</option><option value="circle">圆点</option></Select></label>
        <label className="subscription-field"><span>颜色</span><Select value={active.color} onChange={event=>onUpdateView(active.id,{color:event.target.value})}><option value="teal">青绿</option><option value="blue">蓝</option><option value="green">绿</option><option value="amber">琥珀</option><option value="rose">玫瑰</option><option value="gray">灰</option></Select></label>
        <label className="inbox-checkbox"><Checkbox type="checkbox" checked={active.default} onChange={event=>onUpdateView(active.id,{default:event.target.checked})}/>设为默认视图</label>
        <Button variant="quiet" iconOnly className="icon-button danger" type="button" aria-label={`删除视图 ${active.name}`} title="删除视图" onClick={()=>onDeleteView(active.id)}><Trash2 size={16}/></Button>
      </div>})()}
    </section>
    {inbox.error&&<p className="inline-feedback" role="alert">{inbox.error} 上次确认的结果仍保留。</p>}
    {inbox.loading&&<p className="field-hint" role="status">正在读取本地历史…</p>}
    {readStates.error&&<p className="inline-feedback" role="alert">{readStates.error}<Button variant="secondary" className="button button-secondary" onClick={readStates.refresh}>重新读取已读状态</Button></p>}
    {saveNotice&&<p className="inline-feedback collection-save-feedback" role={saveError?'alert':'status'}>{saveNotice}</p>}
    {savingUnavailable&&<p className="field-hint">资料库暂时无法读取，收藏操作不可用。</p>}
    {inbox.confirmed&&<>
      <div className="subscription-reading-toolbar"><span className="inbox-count">共 {inbox.count} 条 · 第 {inbox.pageNumber} 页 · 当前 {inbox.page.entries.length} 条</span><Button variant="secondary" className="button button-secondary" aria-label="将本页标为已读" title="将本页标为已读" disabled={busy||!inbox.page.entries.some(entry=>readStates.get(entry.item.id)?.read===false)} onClick={()=>void readStates.setRead(inbox.page.entries.map(entry=>entry.item.id),true)}><CheckCheck size={15}/>将本页标为已读</Button></div>
      {inbox.page.entries.length?<CardCollection className="content-collection subscription-gallery" masonry={false}>{inbox.page.entries.map(entry=><div className="inbox-entry" key={entry.item.id}><ContentCard headingLevel={3} item={entry.item} saved={savedIDs.has(entry.item.id)} pending={savingUnavailable||savingIDs.has(entry.item.id)} onRead={onRead} onSave={onSave} status={<span className="subscription-read-status">{readStates.available?readStates.get(entry.item.id)?.read?<><Check size={12}/>已读</>:<><span className="unread-dot"/>未读</>:'状态待读取'}</span>} actions={<Button variant="quiet" disabled={busy} aria-label={`${readStates.get(entry.item.id)?.read?'标为未读':'标为已读'} ${entry.item.title}`} onClick={()=>void readStates.setRead([entry.item.id],!readStates.get(entry.item.id)?.read)}>{readStates.get(entry.item.id)?.read?<CheckCheck size={16} aria-hidden="true"/>:<Circle size={15} aria-hidden="true"/>}<span>{readStates.get(entry.item.id)?.read?'标为未读':'标为已读'}</span></Button>}/><p className="inbox-provenance">{entry.sources.map(source=><span key={source.id}>{source.name}{source.removed?'（已移除）':''}</span>)}</p></div>)}</CardCollection>:<div className="subscription-empty"><h3>这里暂时没有内容</h3><p>更新订阅后会留下获取过的条目，也可调整筛选查找已有历史。</p></div>}
      {(inbox.hasPrevious||inbox.page.nextCursor)&&<nav className="inbox-pagination" aria-label="历史分页"><Button variant="secondary" className="button button-secondary" disabled={inbox.loading||!inbox.hasPrevious} onClick={inbox.back}><ChevronLeft size={16}/>上一页</Button><span>第 {inbox.pageNumber} 页</span><Button variant="secondary" className="button button-secondary" disabled={inbox.loading||!inbox.page.nextCursor} onClick={()=>void inbox.next()}>下一页<ChevronRight size={16}/></Button></nav>}
    </>}
    <p className="subscription-reading-hint">仅保留实际获取过的条目和来源提供的文字、媒体地址。移除订阅会保留历史；平台上的全历史和媒体文件需另行获取。</p>
  </section>
}
