import { Button, Dialog } from '../../components/ui'
import { ArrowLeft, ArrowRight, ImageOff, RotateCw, Maximize2, X, ZoomIn, ZoomOut } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { ContentItem } from '../../shared/api/types'
import { retryThroughProxy, sourceImageURL } from '../../shared/media/sourceImage'
import './image-album.css'

type AlbumImage = NonNullable<ContentItem['images']>[number]

type RefreshProps = { onRefresh?: () => void; onUpdateSource?: () => void; refreshing?: boolean }

function Picture({ item, image, source, label, onRefresh, onUpdateSource, refreshing }: { item: ContentItem; image: AlbumImage; source: string; label: string } & RefreshProps) {
  const [failed,setFailed] = useState(false), [attempt,setAttempt] = useState(0)
  return failed ? <div className="album-image-error" role="status"><ImageOff size={26} aria-hidden="true"/><p>图片暂时无法显示，正文与笔记仍可阅读。</p>{onRefresh && <Button variant="secondary" className="button button-secondary" disabled={refreshing} onClick={onRefresh}><RotateCw size={15} aria-hidden="true"/>{refreshing?'正在更新':'更新图集'}</Button>}{onUpdateSource && <Button variant="secondary" className="button button-secondary" disabled={refreshing} onClick={onUpdateSource}>更新链接</Button>}<Button variant="secondary" className="button button-secondary" disabled={refreshing} onClick={()=>{setFailed(false);setAttempt(value=>value+1)}}><RotateCw size={15} aria-hidden="true"/>重试图片</Button></div> :
    <img key={attempt} className="album-main-image" src={source} alt={image.alt||label} width={image.width||undefined} height={image.height||undefined} decoding="async" referrerPolicy="no-referrer" onError={event=>retryThroughProxy(event,item,image.url)||setFailed(true)}/>
}

export function ImageAlbum({ item, onRefresh, onUpdateSource, refreshing }: { item: ContentItem } & RefreshProps) {
  const images = item.images?.length ? item.images : [{url:item.mediaUrl||item.thumbnailUrl}].filter(image=>image.url)
  const [selected,setSelected] = useState(0)
  const [expanded, setExpanded] = useState(false), [originalSize, setOriginalSize] = useState(false)
  const dialog = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLButtonElement>(null)
  const index = Math.min(selected,images.length-1)
  useEffect(() => {
    if (!expanded) return
    const node = dialog.current
    node?.showModal()
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { node?.close(); document.body.style.overflow = overflow; opener.current?.focus({ preventScroll: true }) }
  }, [expanded])
  useEffect(() => { setOriginalSize(false) }, [index])
  if (!images.length) return <p className="field-hint">来源没有提供可显示的图片。</p>
  return <section className="image-album" aria-label="图片浏览" tabIndex={0} onKeyDown={event=>{
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || (event.target as Element).closest('button, a, input, textarea, select')) return
    if (event.key==='ArrowLeft') {event.preventDefault();setSelected(Math.max(0,index-1))}
    else if (event.key==='ArrowRight') {event.preventDefault();setSelected(Math.min(images.length-1,index+1))}
    else if(event.key==='Home'){event.preventDefault();setSelected(0)}
    else if(event.key==='End'){event.preventDefault();setSelected(images.length-1)}
  }}>
    <div className="album-stage"><Picture key={images[index].url+'-'+index} item={item} image={images[index]} source={sourceImageURL(item,images[index].url)} label={`${item.title}，第 ${index+1} 张图片`} onRefresh={onRefresh} onUpdateSource={onUpdateSource} refreshing={refreshing}/></div>
    <div className="album-controls"><span role="status" aria-live="polite">第 {index+1} / {images.length} 张</span><div><Button variant="quiet" iconOnly ref={opener} className="icon-button" aria-label="放大查看图片" title="放大查看图片" onClick={()=>setExpanded(true)}><Maximize2 size={17}/></Button><Button variant="quiet" iconOnly className="icon-button" aria-label="上一张图片" disabled={index===0} onClick={()=>setSelected(index-1)}><ArrowLeft size={17} aria-hidden="true"/></Button><Button variant="quiet" iconOnly className="icon-button" aria-label="下一张图片" disabled={index===images.length-1} onClick={()=>setSelected(index+1)}><ArrowRight size={17} aria-hidden="true"/></Button></div></div>
    {images.length>1 && <div className="album-thumbnails" aria-label="图片顺序">{images.map((image,i)=><Button variant="quiet" key={i} aria-label={`查看第 ${i+1} 张图片`} aria-pressed={index===i} onClick={()=>setSelected(i)}><img src={sourceImageURL(item,image.url)} alt="" loading="lazy" decoding="async" fetchPriority="low" referrerPolicy="no-referrer"/><span>{i+1}</span></Button>)}</div>}
    {expanded && <Dialog ref={dialog} className="album-dialog" aria-label="放大图片" onClose={()=>setExpanded(false)}>
      <div className="album-dialog-toolbar"><span>第 {index+1} / {images.length} 张</span><Button variant="quiet" iconOnly className="icon-button" title="上一张图片" aria-label="上一张图片" disabled={index===0} onClick={()=>setSelected(index-1)}><ArrowLeft size={18}/></Button><Button variant="quiet" iconOnly className="icon-button" title="下一张图片" aria-label="下一张图片" disabled={index===images.length-1} onClick={()=>setSelected(index+1)}><ArrowRight size={18}/></Button><Button variant="quiet" iconOnly className="icon-button" title={originalSize?'适应窗口':'原始大小'} aria-label={originalSize?'适应窗口':'原始大小'} aria-pressed={originalSize} onClick={()=>setOriginalSize(value=>!value)}>{originalSize?<ZoomOut size={18}/>:<ZoomIn size={18}/>}</Button><Button variant="quiet" iconOnly className="icon-button" title="关闭图片" aria-label="关闭图片" onClick={()=>setExpanded(false)}><X size={20}/></Button></div>
      <div className={`album-dialog-image${originalSize?' original-size':''}`}><Picture key={images[index].url} item={item} image={images[index]} source={sourceImageURL(item,images[index].url)} label={`${item.title}，第 ${index+1} 张图片`} onRefresh={onRefresh} onUpdateSource={onUpdateSource} refreshing={refreshing}/></div>
    </Dialog>}
  </section>
}
