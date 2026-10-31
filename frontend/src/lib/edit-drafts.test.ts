import { describe, expect, it } from 'vitest'
import { dropDraft, keepDraft, takeDraft } from './edit-drafts'

describe('edit drafts', () => {
  it('returns an edit started from the same content', () => {
    keepDraft('/a.ini', 'h1', 'saved', 'edited')
    expect(takeDraft('/a.ini', 'h1')).toBe('edited')
  })

  it('drops a draft whose file has changed since', () => {
    keepDraft('/b.ini', 'h1', 'saved', 'edited')
    expect(takeDraft('/b.ini', 'h2')).toBeNull()
    expect(takeDraft('/b.ini', 'h1')).toBeNull()
  })

  it('forgets a draft that is back to the saved content', () => {
    keepDraft('/c.ini', 'h1', 'saved', 'edited')
    keepDraft('/c.ini', 'h1', 'saved', 'saved')
    expect(takeDraft('/c.ini', 'h1')).toBeNull()
  })

  it('can be dropped', () => {
    keepDraft('/d.ini', 'h1', 'saved', 'edited')
    dropDraft('/d.ini')
    expect(takeDraft('/d.ini', 'h1')).toBeNull()
  })
})
