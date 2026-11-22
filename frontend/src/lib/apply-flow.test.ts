import { describe, expect, it } from 'vitest'
import { diffCount, limitDiff, groupRestartNote, previewMessage, resultSummary } from './apply-flow'

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

describe('groupRestartNote', () => {
  it('names [group:] groups only, not numprocs copies or single programs', () => {
    const ps = [
      { Group: 'app', Program: 'app-horizon' },
      { Group: 'app', Program: 'app-pulse' },
      { Group: 'app', Program: 'app-scheduler' },
      { Group: 'queue', Program: 'queue' },
      { Group: 'queue', Program: 'queue' },
      { Group: 'web', Program: 'web' },
    ]
    expect(groupRestartNote(ps)).toBe('In a group, supervisord restarts every process when any of its programs changes, as supervisorctl update does: app (3 processes).')
    expect(groupRestartNote(ps.slice(3))).toBe('')
    expect(groupRestartNote(null)).toBe('')
  })
})
