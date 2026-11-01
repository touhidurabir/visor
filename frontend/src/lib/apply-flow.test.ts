import { describe, expect, it } from 'vitest'
import { diffCount, limitDiff, previewMessage, resultSummary } from './apply-flow'

describe('apply flow text', () => {
  const d = { Added: ['new'], Changed: ['a', 'b'], Removed: null }
  it('counts and limits a diff', () => {
    expect(diffCount(d)).toBe(3)
    expect(limitDiff(d, ['a', 'zzz'])).toEqual({ Added: [], Changed: ['a'], Removed: [] })
    expect(limitDiff(d, null)).toBe(d)
  })
  it('lists each kind of change', () => {
    const m = previewMessage(d)
    expect(m).toContain('Added: new.')
    expect(m).toContain('Changed: a, b.')
    expect(m).not.toContain('Removed')
  })
  it('summarises a result', () => {
    expect(resultSummary({ Added: ['x'], Changed: ['y', 'z'], Removed: [], Errors: [] })).toBe('1 added, 2 restarted')
    expect(resultSummary({ Added: null, Changed: null, Removed: null, Errors: null })).toBe('nothing to change')
  })
})
