import { describe, expect, it } from 'vitest'
import { discussionSummary, nextDiscussionPostIDs } from './DiscussionReader'

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
    expect(discussionSummary(discussion)).toContain('#2 bob\n回复')
  })
})
