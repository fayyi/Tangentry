import { describe, expect, it } from 'vitest'
import { discussionSummary, discussionTargetLoaded, mainPostDiscussion, nextDiscussionPostIDs, shouldLoadDiscussionTarget } from './DiscussionReader'

const discussion = {
  topicId: '42', totalPosts: 4, loadedPosts: 2, postIds: [101, 102, 103, 104],
  posts: [
    { id: 101, number: 1, username: 'alice', createdAt: 1, blocks: [{ kind: 'paragraph' as const, text: '主帖' }] },
    { id: 102, number: 2, username: 'bob', createdAt: 2, blocks: [{ kind: 'paragraph' as const, text: '回复' }] },
  ],
}

describe('DiscussionReader helpers', () => {
  it('calculates only unloaded post ids in bounded batches', () => {
    expect(nextDiscussionPostIDs(discussion, 1)).toEqual([103])
    expect(nextDiscussionPostIDs({ ...discussion, posts: [...discussion.posts, { id: 103, number: 3, username: 'c', createdAt: 3, blocks: [] }] }, 20)).toEqual([104])
  })
  it('builds AI input with floor and author labels', () => {
    expect(discussionSummary(discussion)).toContain('#1 alice\n主帖')
    expect(discussionSummary(discussion)).not.toContain('#2 bob\n回复')
    expect(discussionSummary(discussion, true)).toContain('#2 bob\n回复')
  })
  it('keeps only the main post until replies are requested', () => {
    const main = mainPostDiscussion(discussion)
    expect(main.posts.map(post => post.number)).toEqual([1])
    expect(main.loadedPosts).toBe(1)
    expect(shouldLoadDiscussionTarget(main, { postNumber: 3 })).toBe(true)
    expect(discussionTargetLoaded(main, { postNumber: 1 })).toBe(true)
  })
  it('loads a note target only when it is not already visible', () => {
    expect(shouldLoadDiscussionTarget(discussion, { postId: 102 })).toBe(false)
    expect(shouldLoadDiscussionTarget(discussion, { postNumber: 4 })).toBe(true)
    expect(shouldLoadDiscussionTarget({ ...discussion, posts: [...discussion.posts, ...Array.from({ length: 2 }, (_, index) => ({ id: 103 + index, number: 3 + index, username: 'reply', createdAt: 0, blocks: [] }))] }, { postNumber: 4 })).toBe(false)
  })
})
