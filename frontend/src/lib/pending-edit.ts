// A fix chosen on a config's Overview, waiting for the Config tab editor
// to open: the editor applies it to its text (nothing is saved) and goes
// to the line, so the change is reviewed and saved like any other edit.
import type { EditView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export type PendingEdit = { edit: EditView } | { after: number; lines: string[] }

const pending = new Map<string, PendingEdit>()

export function setPendingEdit(path: string, p: PendingEdit) {
  pending.set(path, p)
}

/** Returns and forgets the fix waiting for path, if any. */
export function takePendingEdit(path: string): PendingEdit | undefined {
  const p = pending.get(path)
  pending.delete(path)
  return p
}
