// The apply flow shared by the banner, the menus and the tray popover: show
// what supervisord will add, restart or remove, ask, apply, then report. The
// text is pure (and tested); useApplyFlow holds the state around the calls.
import { isCancelled, onProduction, withConsentOnce } from './production'
import { ref } from 'vue'
import { type ApplyResultView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ConfigService, ServerService } from './api'
import { saveErrorText } from './config-errors'
import { describeError } from './faults'

export interface DiffLike {
  Added?: string[] | null
  Changed?: string[] | null
  Removed?: string[] | null
}

const list = (a?: string[] | null) => a ?? []
export const diffCount = (d: DiffLike) => list(d.Added).length + list(d.Changed).length + list(d.Removed).length

/** Keeps only the groups in `only` (all when it is null). */
export function limitDiff(d: DiffLike, only: string[] | null): DiffLike {
  if (!only) return d
  const keep = (a?: string[] | null) => list(a).filter((g) => only.includes(g))
  return { Added: keep(d.Added), Changed: keep(d.Changed), Removed: keep(d.Removed) }
}

/** The confirm text: one line per kind of change, naming the groups. */
export function previewMessage(d: DiffLike): string {
  const lines: string[] = []
  if (list(d.Added).length) lines.push(`Added: ${list(d.Added).join(', ')}. They start if autostart is on.`)
  if (list(d.Changed).length) lines.push(`Changed: ${list(d.Changed).join(', ')}. They restart with the new settings.`)
  if (list(d.Removed).length) lines.push(`Removed: ${list(d.Removed).join(', ')}. They stop and go.`)
  return lines.join('\n')
}

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

/** One sentence for what an apply did; the failures are listed apart. */
export function resultSummary(r: ApplyResultView): string {
  const parts = [
    list(r.Added).length && `${list(r.Added).length} added`,
    list(r.Changed).length && `${list(r.Changed).length} restarted`,
    list(r.Removed).length && `${list(r.Removed).length} removed`,
  ].filter(Boolean)
  return parts.length ? parts.join(', ') : 'nothing to change'
}

export interface Pending {
  title: string
  message: string
  confirmLabel: string
  danger: boolean
}

export interface Report {
  title: string
  /** Per-group failures. */
  errors: string[]
  failed: boolean
  /** Puts back what a remove took away. */
  undo?: () => Promise<void>
}

export function useApplyFlow(onDone: () => void = () => {}) {
  const pending = ref<Pending | null>(null)
  const running = ref(false)
  const report = ref<Report | null>(null)
  let go: (() => Promise<void>) | null = null

  async function guarded(fn: () => Promise<void>) {
    if (running.value) return
    running.value = true
    try {
      await fn()
    } catch (e) {
      report.value = { title: describeError(e), errors: [], failed: true }
    } finally {
      running.value = false
      onDone()
    }
  }

  function finish(title: string, res: ApplyResultView, undo?: Report['undo']) {
    const errors = list(res.Errors)
    report.value = {
      title: errors.length ? `${title}, but ${plural(errors.length, 'group')} could not apply` : `${title}: ${resultSummary(res)}.`,
      errors,
      failed: errors.length > 0,
      undo,
    }
  }

  /**
   * Asks supervisord what differs, and confirms before applying `only` (all
   * when null). `run` gets the groups the confirm listed.
   */
  function apply(label: string, only: string[] | null, run: (shown: string[]) => Promise<ApplyResultView>) {
    return guarded(async () => {
      report.value = null
      const diff = limitDiff(await ServerService.CheckForChanges(), only)
      if (diffCount(diff) === 0 && !only) {
        report.value = { title: 'Nothing to apply: supervisord already runs every config.', errors: [], failed: false }
        return
      }
      const message = previewMessage(diff) || `${label} has no changes supervisord can see yet. Applying re-reads it and updates its processes.`
      const shown = [...list(diff.Added), ...list(diff.Changed), ...list(diff.Removed)]
      go = async () => finish('Applied', await run(shown))
      pending.value = { title: `Apply changes to ${label}${onProduction()}?`, message, confirmLabel: 'Apply changes', danger: false }
    })
  }

  /**
   * Banner, server menu and tray: everything that differs, as the confirm
   * listed it. A change that turns up after the confirm waits for the next
   * apply (and its config keeps its badge).
   */
  const applyAll = () => apply('every changed config', null, (shown) => ServerService.ApplyChanges(shown))

  /** Config menu: this file's groups (new ones too). */
  const applyFile = (path: string, fileName: string, groups: string[]) =>
    apply(fileName, groups, () => ConfigService.ApplyFile(path))

  /** Config menu: its own confirm (ActionMenu) has been answered. */
  function removeFile(path: string, fileName: string) {
    return guarded(async () => {
      report.value = null
      const res = await ServerService.RemoveFromSupervisor(path)
      const undo = async () => {
        try {
          const backups = await ConfigService.Backups(path)
          const last = (backups ?? []).find((b) => b.Removed)
          if (!last) throw new Error('Visor can’t find the backup of that file.')
          finish(`Restored ${fileName}`, await ConfigService.Restore(path, last.ID))
        } catch (e) {
          if (isCancelled(e)) return // a production question answered No: the report stays
          // Keep Undo, so it can be tried again once the cause is fixed.
          report.value = { title: saveErrorText(e, fileName), errors: [], failed: true, undo }
        }
      }
      finish(`Removed ${fileName} from supervisor`, res, undo)
    })
  }

  function confirm() {
    const run = go
    pending.value = null
    go = null
    // Its confirm asked: a production server doesn't ask again.
    if (run) void withConsentOnce(() => guarded(run))
  }
  function cancel() {
    pending.value = null
    go = null
  }
  async function undo() {
    const u = report.value?.undo
    if (u) await guarded(u)
  }

  return { pending, running, report, applyAll, applyFile, removeFile, confirm, cancel, undo, dismiss: () => (report.value = null) }
}
