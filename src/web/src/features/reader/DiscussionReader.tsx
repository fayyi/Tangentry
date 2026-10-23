import { Button } from '../../components/ui'
import { CornerUpLeft, Heart, LoaderCircle } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { Discussion, DiscussionPost, ReadingNote, TextAnchor } from '../../shared/api/types'
import { request } from '../../shared/api/client'
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

export function discussionSummary(discussion: Discussion, includeReplies = false) {
  const posts = includeReplies ? discussion.posts : discussion.posts.slice(0, 1)
  return posts.map(post => `#${post.number} ${post.username}\n${postText(post)}`).join('\n\n')
}

/** Keep topic metadata and the first post while deferring reply content. */
export function mainPostDiscussion(discussion: Discussion): Discussion {
  return { ...discussion, loadedPosts: discussion.posts.length > 0 ? 1 : 0, posts: discussion.posts.slice(0, 1) }
}

export function discussionTargetLoaded(discussion: Discussion, anchor?: Pick<TextAnchor, 'postNumber' | 'postId'> | null) {
  if (!anchor || (anchor.postNumber === undefined && anchor.postId === undefined)) return false
  return discussion.posts.some(post =>
    (anchor.postId !== undefined && post.id === anchor.postId) ||
    (anchor.postNumber !== undefined && post.number === anchor.postNumber),
  )
}

export function shouldLoadDiscussionTarget(discussion: Discussion, anchor?: Pick<TextAnchor, 'postNumber' | 'postId'> | null) {
  return Boolean(anchor && !discussionTargetLoaded(discussion, anchor) && discussion.posts.length < discussion.totalPosts)
}

export function nextDiscussionPostIDs(discussion: Discussion, batchSize = 20) {
  const loaded = new Set(discussion.posts.map(post => post.id))
  return (discussion.postIds ?? []).filter(id => !loaded.has(id)).slice(0, Math.max(1, Math.min(50, batchSize)))
}

export function DiscussionReader({ discussion: initialDiscussion, sourceURL, viewCount, likeCount, notes, focus, onQuote, onOpenNotes, onUnresolved, onDiscussionChange }: {
  discussion: Discussion
  sourceURL: string
  viewCount?: number
  likeCount?: number
  notes: ReadingNote[]
  focus?: { id: string; token: number; allowReturn?: boolean } | null
  onQuote?: (text: string, anchor?: TextAnchor) => void
  onOpenNotes?: (ids: string[]) => void
  onUnresolved?: (ids: string[]) => void
  onDiscussionChange?: (discussion: Discussion) => void
}) {
  const [discussion, setDiscussion] = useState(() => mainPostDiscussion(initialDiscussion))
  const [loading, setLoading] = useState(false)
  const [repliesRequested, setRepliesRequested] = useState(false)
  const [focusedPost, setFocusedPost] = useState<number | null>(null)
  const sentinel = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLElement>(null)
  const first = discussion.posts[0]
  useEffect(() => {
    setDiscussion(mainPostDiscussion(initialDiscussion))
    setLoading(false)
    setRepliesRequested(false)
    setFocusedPost(null)
  }, [initialDiscussion.topicId])
  const loadMore = async () => {
    if (loading) return
    const ids = nextDiscussionPostIDs(discussion)
    if (!ids.length) return
    setRepliesRequested(true)
    setLoading(true)
    try {
      const loaded = await request<Discussion>('/discovery/linuxdo/posts?topic=' + encodeURIComponent(discussion.topicId) + ids.map(id => '&post_ids[]=' + encodeURIComponent(id)).join(''))
      setDiscussion(current => {
        const seen = new Set(current.posts.map(post => post.id))
        const posts = [...current.posts, ...loaded.posts.filter(post => !seen.has(post.id))]
        return { ...current, loadedPosts: posts.length, posts }
      })
    } finally { setLoading(false) }
  }
  useEffect(() => {
    if (!sentinel.current || !repliesRequested) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) void loadMore() }, { rootMargin: '500px' })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [discussion, loading, repliesRequested])
  const jump = (number: number) => {
    const node = root.current?.querySelector<HTMLElement>(`[data-post-number="${number}"]`)
    if (!node) return
    setFocusedPost(number)
    node.scrollIntoView({ behavior: 'smooth', block: 'start' })
    window.setTimeout(() => setFocusedPost(current => current === number ? null : current), 1600)
  }
  useEffect(() => {
    const anchor = focus && notes.find(note => note.id === focus.id)?.textAnchor
    if (!anchor) return
    const target = discussion.posts.find(post =>
      (anchor.postId !== undefined && post.id === anchor.postId) ||
      (anchor.postNumber !== undefined && post.number === anchor.postNumber),
    )
    if (target) {
      jump(target.number)
      return
    }
    if (shouldLoadDiscussionTarget(discussion, anchor) && !loading) void loadMore()
  }, [discussion, focus?.token, notes, loading])
  useEffect(() => {
    if (repliesRequested) onDiscussionChange?.(discussion)
  }, [discussion, onDiscussionChange, repliesRequested])
  if (!first) return null
  return <section className="discussion-reader" ref={root} aria-label="话题楼层">
    <header className="discussion-stats"><span>{viewCount ?? 0} 浏览</span><span>{likeCount ?? 0} 赞</span><span>{discussion.participatingUsers ?? 0} 位参与者</span><span>已加载 {discussion.posts.length} / 共 {discussion.totalPosts} 楼</span></header>
    {discussion.posts.map((post, index) => <article key={post.id} data-post-number={post.number} className={`discussion-post${focusedPost === post.number ? ' discussion-post-focused' : ''}`}>
      <header className="discussion-post-header">
        <div className="discussion-post-byline"><strong>{post.name || post.username}</strong><span>@{post.username}</span>{index === 0 && <b>楼主</b>}</div>
        <time dateTime={post.createdAt ? new Date(post.createdAt * 1000).toISOString() : undefined} title={post.createdAt ? new Date(post.createdAt * 1000).toLocaleString('zh-CN') : undefined}>{relativeTime(post.createdAt)}</time><a href={`#post-${post.number}`} className="discussion-post-number">#{post.number}</a>
      </header>
      {post.replyToNumber && post.replyToNumber > 0 && <Button variant="quiet" type="button" className="discussion-reply-link" onClick={() => jump(post.replyToNumber!)}><CornerUpLeft size={14} aria-hidden="true"/>回复 @{post.replyToUser || '用户'} #{post.replyToNumber}</Button>}
      <ArticleReader contentId={`${discussion.topicId}:post:${post.id}`} title="" text={postText(post)} blocks={post.blocks} feed={false} notes={notes} focus={focus} postNumber={post.number} postId={post.id} onQuote={(text, anchor) => onQuote?.(text, anchor ? { ...anchor, postNumber: post.number, postId: post.id } : anchor)} onOpenNotes={onOpenNotes} onUnresolved={onUnresolved} sourceURL={sourceURL}/>
      <footer className="discussion-post-footer"><span><Heart size={14} aria-hidden="true"/>{post.likeCount ?? 0}</span></footer>
    </article>)}
    {!repliesRequested && discussion.posts.length < discussion.totalPosts
      ? <Button variant="secondary" type="button" className="button button-secondary discussion-load-replies" onClick={() => void loadMore()} disabled={loading}>加载回复（共 {Math.max(0, discussion.totalPosts - 1)} 条）</Button>
      : <div ref={sentinel} className="discussion-load-status">{loading ? <><LoaderCircle size={15} className="spin"/>正在加载后续楼层…</> : discussion.posts.length < discussion.totalPosts ? `已加载 ${discussion.posts.length} / 共 ${discussion.totalPosts} 楼` : '已显示全部楼层'}</div>}
  </section>
}
