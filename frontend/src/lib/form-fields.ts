// The fields of "Fill a form": sections, labels and kinds, in the order of
// the wireframe. `key` is the supervisord setting ("name" is the program name).
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'

export interface FormField {
  key: string
  label: string
  options?: string[] // a select when set
  mono?: boolean
  wide?: boolean
  help: string
}

export interface FormSection {
  title: string
  fields: FormField[]
}

const BOOL = ['true', 'false']

export const FORM_SECTIONS: FormSection[] = [
  {
    title: 'Basics',
    fields: [
      { key: 'name', label: 'Program name', help: 'Letters, digits, - _ and . only' },
      { key: 'command', label: 'Command', mono: true, wide: true, help: 'What supervisord runs' },
      { key: 'directory', label: 'Working folder', mono: true, help: 'Optional. The folder the command runs in' },
      { key: 'user', label: 'Run as user', help: 'Blank runs it as the user supervisord runs as' },
    ],
  },
  {
    title: 'Starting and restarting',
    fields: [
      { key: 'autostart', label: 'Start with supervisord', options: BOOL, help: '' },
      { key: 'autorestart', label: 'Restart when it exits', options: ['true', 'unexpected', 'false'], help: '' },
      { key: 'numprocs', label: 'Copies', help: 'How many copies to run' },
      { key: 'startsecs', label: 'Seconds before it counts as started', help: '' },
      { key: 'startretries', label: 'Start attempts', help: '' },
      { key: 'process_name', label: 'Copy name', mono: true, wide: true, help: 'Name of each copy' },
    ],
  },
  {
    title: 'Stopping',
    fields: [
      { key: 'stopwaitsecs', label: 'Seconds to stop gracefully', help: '' },
      { key: 'stopsignal', label: 'Stop signal', options: ['TERM', 'INT', 'QUIT', 'HUP', 'KILL', 'USR1', 'USR2'], help: '' },
      { key: 'stopasgroup', label: 'Stop child processes too', options: BOOL, help: '' },
      { key: 'killasgroup', label: 'Kill child processes too', options: BOOL, help: '' },
    ],
  },
  {
    title: 'Logs and environment',
    fields: [
      { key: 'stdout_logfile', label: 'Output log', mono: true, wide: true, help: 'Blank: auto-named file in supervisord’s log folder' },
      { key: 'redirect_stderr', label: 'Send errors to the output log', options: BOOL, help: '' },
      { key: 'stderr_logfile', label: 'Error log', mono: true, wide: true, help: 'Blank: auto-named file in supervisord’s log folder' },
      { key: 'environment', label: 'Environment variables', mono: true, wide: true, help: 'KEY="value" pairs, comma separated' },
      { key: 'priority', label: 'Start order (priority)', help: 'Lower numbers start first' },
    ],
  },
]

export type FieldLevel = 'ok' | 'warning' | 'error'
export interface FieldNote {
  level: FieldLevel
  text: string
}

const RANK: Record<FieldLevel, number> = { error: 0, warning: 1, ok: 2 }

/**
 * Maps the checks onto the form's fields by Check.Key. A check about the
 * program as a whole (no key: "already exists in x.ini") belongs to the name
 * field; the "needs %(process_num)" error on numprocs also shows on the copy
 * name. The worst finding per field wins. Checks about the file itself
 * (no program) are not field notes; see fileChecks.
 */
export function fieldNotes(checks: Check[]): Record<string, FieldNote> {
  const out: Record<string, FieldNote> = {}
  const put = (key: string, c: Check) => {
    const cur = out[key]
    if (!cur || RANK[c.Level as FieldLevel] < RANK[cur.level]) out[key] = { level: c.Level as FieldLevel, text: c.Message }
  }
  for (const c of checks) {
    if (!c.Program && !c.Key) continue
    put(c.Key === '' ? 'name' : c.Key, c)
    if (c.Key === 'numprocs' && c.Level === 'error' && c.Message.includes('process_num')) put('process_name', c)
  }
  return out
}

/** Checks that are about the file, not one program (wrong extension, no program). */
export function fileChecks(checks: Check[]): Check[] {
  return checks.filter((c) => !c.Program && !c.Key)
}

const NAME_OK = /^[A-Za-z0-9_.-]+$/

/** Errors the form itself can tell before Go answers: an empty or odd program name. */
export function nameProblem(name: string): string | null {
  if (!name.trim()) return 'Required'
  if (!NAME_OK.test(name.trim())) return 'Use letters, digits, - _ and . only'
  return null
}
