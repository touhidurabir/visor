// What went wrong with a save, from the sentence Go's error carries. The
// messages come from apply.ErrChangedOnDisk, apply.ErrNotWritable,
// app.ErrInvalidConfig and app.ErrRejected (internal/apply,
// internal/app/config_service.go).
import { describeError } from './faults'

export type SaveFailure = 'changed' | 'readonly' | 'invalid' | 'rejected' | 'other'

export function classifySaveError(e: unknown): SaveFailure {
  const text = e instanceof Error ? e.message : String(e)
  if (/file changed on disk/.test(text)) return 'changed'
  if (/config folder is not writable/.test(text)) return 'readonly'
  if (/config has errors/.test(text)) return 'invalid'
  // supervisord refused the file when it re-read its config; Visor put the
  // previous version back (the edits stay in the editor).
  if (/supervisord refused this config/.test(text)) return 'rejected'
  return 'other'
}

/** The sentence to show for a failed save or restore. */
export function saveErrorText(e: unknown, file: string): string {
  switch (classifySaveError(e)) {
    case 'changed':
      return `${file} changed on disk after you opened it, so Visor did not save over it.`
    case 'readonly':
      return `Visor can’t write ${file}: the file or its folder is read-only for you.`
    case 'invalid': {
      const text = e instanceof Error ? e.message : String(e)
      const m = /config has errors: (.*)$/s.exec(text)
      return m ? `Fix this first: ${m[1]}` : 'Fix the errors in the checks first.'
    }
    case 'rejected': {
      const text = e instanceof Error ? e.message : String(e)
      const m = /supervisord refused this config: (.*)$/s.exec(text)
      return m ? `supervisord refused ${file}: ${m[1]}` : text
    }
    default:
      return describeError(e)
  }
}
