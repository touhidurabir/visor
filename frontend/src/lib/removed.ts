// "Recently removed" on the dashboard: config files removed from supervisor
// that are not back, each with the backup that restores it (I-5).
import type { RemovedView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { historyTime } from './format'

export interface RemovedRow {
  path: string
  fileName: string
  backupID: string
  /** The folder it was in, for telling same-named files apart. */
  folder: string
  /** "Removed Today 07:48". */
  when: string
}

export function removedRows(list: RemovedView[] | null | undefined, nowMs: number): RemovedRow[] {
  return (list ?? []).map((r) => ({
    path: r.Path,
    fileName: r.FileName,
    backupID: r.BackupID,
    folder: r.Path.slice(0, Math.max(0, r.Path.length - r.FileName.length - 1)),
    when: `Removed ${historyTime(new Date(r.At).getTime(), nowMs)}`,
  }))
}
