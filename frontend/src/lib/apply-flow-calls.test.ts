import { beforeEach, describe, expect, it, vi } from 'vitest'

// The generated bindings, mocked: these tests check what useApplyFlow asks Go.
const calls = {
  CheckForChanges: vi.fn(),
  ApplyChanges: vi.fn(),
  RemoveFromSupervisor: vi.fn(),
  Backups: vi.fn(),
  Restore: vi.fn(),
  ApplyFile: vi.fn(),
}
vi.mock('../../bindings/github.com/touhidurabir/visor/internal/app', () => ({
  ServerService: {
    CheckForChanges: () => calls.CheckForChanges(),
    ApplyChanges: (g: string[] | null) => calls.ApplyChanges(g),
    RemoveFromSupervisor: (p: string) => calls.RemoveFromSupervisor(p),
  },
  ConfigService: {
    Backups: (p: string) => calls.Backups(p),
    Restore: (p: string, id: string) => calls.Restore(p, id),
    ApplyFile: (p: string) => calls.ApplyFile(p),
  },
}))

const { useApplyFlow } = await import('./apply-flow')
const ok = { Added: [], Changed: [], Removed: [], Errors: [] }
const flush = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  for (const f of Object.values(calls)) f.mockReset()
})

describe('useApplyFlow', () => {
  // M-10: what the confirm showed is what gets applied, nothing that
  // turned up since.
  it('applies exactly the groups the confirm listed', async () => {
    calls.CheckForChanges.mockResolvedValue({ Added: ['new'], Changed: ['web'], Removed: ['old'] })
    calls.ApplyChanges.mockResolvedValue({ ...ok, Changed: ['web'] })
    const flow = useApplyFlow()
    await flow.applyAll()
    expect(flow.pending.value?.message).toContain('web')
    flow.confirm()
    await flush()
    expect(calls.ApplyChanges).toHaveBeenCalledWith(['new', 'web', 'old'])
    expect(flow.report.value?.failed).toBe(false)
  })

  // I-5: an Undo that fails can be tried again.
  it('keeps Undo when putting the file back fails', async () => {
    calls.RemoveFromSupervisor.mockResolvedValue({ ...ok, Removed: ['web'] })
    calls.Backups.mockResolvedValue([{ ID: 'b1', At: '2026-11-01T09:30:00Z', Removed: true, Content: '' }])
    calls.Restore.mockRejectedValueOnce(new Error('config has errors: [program:web] already exists in other.ini'))
    const flow = useApplyFlow()
    await flow.removeFile('/conf/web.ini', 'web.ini')
    expect(flow.report.value?.undo).toBeTypeOf('function')
    await flow.undo()
    expect(flow.report.value?.failed).toBe(true)
    expect(flow.report.value?.title).toContain('already exists')
    expect(flow.report.value?.undo).toBeTypeOf('function')

    calls.Restore.mockResolvedValueOnce({ ...ok, Added: ['web'] })
    await flow.undo()
    expect(calls.Restore).toHaveBeenLastCalledWith('/conf/web.ini', 'b1')
    expect(flow.report.value?.title).toBe('Restored web.ini: 1 added.')
    expect(flow.report.value?.undo).toBeUndefined()
  })
})
