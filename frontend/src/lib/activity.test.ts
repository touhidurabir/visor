import { describe, expect, it, vi } from 'vitest'
import type { ActivityEventView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

vi.mock('@wailsio/runtime', () => ({ Events: { On: () => () => {} } }))
vi.mock('../../bindings/events', () => ({ VisorActivity: 'visor:activity' }))
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({ IssueAPI: { Activity: () => Promise.resolve({ Server: 'local', Events: [] }) } }))
const { byDay, matches } = await import('./activity')

const ev = (o: Partial<ActivityEventView>): ActivityEventView => ({
  ID: 'x', AtMs: 0, Kind: 'process_restarted', Group: 'processes', Process: 'web:web', Config: '', ByVisor: false, Via: '', Text: 'web:web restarted', FileName: '', ...o,
})

describe('activity', () => {
  it('filters by who, kind and process', () => {
    const e = ev({})
    expect(matches(e, { who: 'all', kind: '', process: '' })).toBe(true)
    expect(matches(e, { who: 'visor', kind: '', process: '' })).toBe(false)
    expect(matches(e, { who: 'outside', kind: 'processes', process: 'web:web' })).toBe(true)
    expect(matches(e, { who: 'all', kind: 'files', process: '' })).toBe(false)
  })
  it('groups by day, newest first', () => {
    const now = new Date(2026, 10, 14, 15, 0).getTime()
    const g = byDay([ev({ AtMs: now - 60_000 }), ev({ AtMs: now - 20 * 3600_000 }), ev({ AtMs: now - 3 * 24 * 3600_000 })], now)
    expect(g.map((x) => x.label).slice(0, 2)).toEqual(['Today', 'Yesterday'])
    expect(g).toHaveLength(3)
  })
})
