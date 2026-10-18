import { CornerUpLeft, Heart, LoaderCircle } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { Discussion, DiscussionPost, ReadingNote, TextAnchor } from '../../shared/api/types'
import { request } from '../../shared/api/client'
import { proxiedImageURL } from '../../shared/media/sourceImage'
import { ArticleReader } from './ArticleReader'

function relativeTime(value: number) {
  if (!value) return ''
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - value))
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  if (seconds < 2592000) return `${Math.floor(seconds / 86400)} 天前`
  return new Date(value * 1000).toLocaleDateString('zh-CN')
}

function postText(post: DiscussionPost) {
  return post.blocks.filter(block => block.kind !== 'image' && block.kind !== 'emoji').map(block => block.text).filter(Boolean).join('\n\n')
}

export function discussionSummary(discussion: Discussion) {
  return discussion.posts.map(post => `#${post.number} ${post.username}\n${postText(post)}`).join('\n\n')
}

export function nextDiscussionPostIDs(discussion: Discussion, batchSize = 20) {
  const loaded = new Set(discussion.posts.map(post => post.id))
  return (discussion.postIds ?? []).filter(id => !loaded.has(id)).slice(0, Math.max(1, Math.min(50, batchSize)))
}

export function DiscussionReader({ discussion: initialDiscussion, topicURL, sourceURL, viewCount, likeCount, notes, focus, onQuote, onOpenNotes, onUnresolved }: {
  discussion: Discussion
  topicURL: string
  sourceURL: string
  viewCount?: number
  likeCount?: number
  notes: ReadingNote[]
  focus?: { id: string; token: number; allowReturn?: boolean } | null
  onQuote?: (text: string, anchor?: TextAnchor) => void
  onOpenNotes?: (ids: string[]) => void
  onUnresolved?: (ids: string[]) => void
}) {
  const [discussion, setDiscussion] = useState(initialDiscussion)
  const [loading, setLoading] = useState(false)
  const [focusedPost, setFocusedPost] = useState<number | null>(null)
  const sentinel = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLElement>(null)
  const first = discussion.posts[0]
  const loadMore = async () => {
    if (loading) return
    const ids = nextDiscussionPostIDs(discussion)
    if (!ids.length) return
    setLoading(true)
    try {
      const loaded = await request<Discussion>('/discovery/linuxdo/posts?topic=' + encodeURIComponent(discussion.topicId) + ids.map(id => '&post_ids[]=' + encodeURIComponent(id)).join(''))
      setDiscussion(current => ({ ...current, loadedPosts: current.posts.length + loaded.posts.length, posts: [...current.posts, ...loaded.posts] }))
    } finally { setLoading(false) }
  }
  useEffect(() => {
    if (!sentinel.current) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) void loadMore() }, { rootMargin: '500px' })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [discussion, loading])
  const jump = (number: number) => {
    const node = root.current?.querySelector<HTMLElement>(`[data-post-number="${number}"]`)
    if (!node) return
    setFocusedPost(number)
    node.scrollIntoView({ behavior: 'smooth', block: 'start' })
    window.setTimeout(() => setFocusedPost(current => current === number ? null : current), 1600)
  }
  useEffect(() => {
    const anchor = focus && notes.find(note => note.id === focus.id)?.textAnchor
    if (anchor?.postNumber) jump(anchor.postNumber)
  }, [focus?.token, notes])
  if (!first) return null
  return <section className="discussion-reader" ref={root} aria-label="话题楼层">
    <header className="discussion-stats"><span>{viewCount ?? 0} 浏览</span><span>{likeCount ?? 0} 赞</span><span>{discussion.participatingUsers ?? 0} 位参与者</span><span>已加载 {discussion.posts.length} / 共 {discussion.totalPosts} 楼</span></header>
    {discussion.posts.map((post, index) => <article key={post.id} data-post-number={post.number} className={`discussion-post${focusedPost === post.number ? ' discussion-post-focused' : ''}`}>
      <header className="discussion-post-header">
        {post.avatarUrl && <img className="discussion-post-avatar" src={proxiedImageURL(post.avatarUrl, topicURL)} alt="" width={40} height={40} loading="lazy" referrerPolicy="no-referrer" />}
        <div className="discussion-post-byline"><strong>{post.name || post.username}</strong><span>@{post.username}</span>{index === 0 && <b>楼主</b>}</div>
        <time dateTime={post.createdAt ? new Date(post.createdAt * 1000).toISOString() : undefined} title={post.createdAt ? new Date(post.createdAt * 1000).toLocaleString('zh-CN') : undefined}>{relativeTime(post.createdAt)}</time><a href={`#post-${post.number}`} className="discussion-post-number">#{post.number}</a>
      </header>
      {post.replyToNumber && post.replyToNumber > 0 && <button type="button" className="discussion-reply-link" onClick={() => jump(post.replyToNumber!)}><CornerUpLeft size={14} aria-hidden="true"/>回复 @{post.replyToUser || '用户'} #{post.replyToNumber}</button>}
      <ArticleReader contentId={`${discussion.topicId}:post:${post.id}`} title="" text={postText(post)} blocks={post.blocks} feed={false} notes={notes} focus={focus} postNumber={post.number} postId={post.id} onQuote={(text, anchor) => onQuote?.(text, anchor ? { ...anchor, postNumber: post.number, postId: post.id } : anchor)} onOpenNotes={onOpenNotes} onUnresolved={onUnresolved} sourceURL={sourceURL}/>
      <footer className="discussion-post-footer"><span><Heart size={14} aria-hidden="true"/>{post.likeCount ?? 0}</span></footer>
    </article>)}
    <div ref={sentinel} className="discussion-load-status">{loading ? <><LoaderCircle size={15} className="spin"/>正在加载后续楼层…</> : discussion.posts.length < discussion.totalPosts ? `已加载 ${discussion.posts.length} / 共 ${discussion.totalPosts} 楼` : '已显示全部楼层'}</div>
  </section>
}
