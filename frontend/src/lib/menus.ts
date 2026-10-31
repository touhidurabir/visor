// Action menus as pure data. Each item's `cmd` is the supervisorctl
// equivalent, shown in grey so people can learn it. Wiring items to service
// calls happens in the views that own them; ActionMenu only emits the id.
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export interface MenuItem {
  id: string
  label: string
  cmd: string
  disabled: boolean
  danger?: boolean
  /** When set, ActionMenu asks for confirmation with this text before emitting. */
  confirm?: string
  /** Tooltip, e.g. why the item is disabled. */
  title?: string
}

const item = (id: string, label: string, cmd: string, disabled = false, extra: Partial<MenuItem> = {}): MenuItem => ({
  id,
  label,
  cmd,
  disabled,
  ...extra,
})

const STARTABLE = ['STOPPED', 'EXITED', 'FATAL']
const STOPPABLE = ['RUNNING', 'STARTING', 'BACKOFF']

export function processMenu(p: ProcessView): MenuItem[] {
  const name = p.FullName
  return [
    item('start', 'Start', `supervisorctl start ${name}`, !STARTABLE.includes(p.State)),
    item('stop', 'Stop', `supervisorctl stop ${name}`, !STOPPABLE.includes(p.State)),
    item('restart', 'Restart', `supervisorctl restart ${name}`, p.State === 'STOPPING'),
    item('signal', 'Send signal…', `supervisorctl signal <sig> ${name}`, p.State !== 'RUNNING'),
    item('logs', 'View logs', `supervisorctl tail -f ${name}`),
    // supervisord clears the logs of a running process only; for any other
    // it answers success and leaves the files alone.
    item('clear', 'Clear logs', `supervisorctl clear ${name}`, p.State !== 'RUNNING', {
      danger: true,
      confirm: `supervisord empties both the stdout and the stderr log files of ${name}. This can’t be undone.`,
      ...(p.State !== 'RUNNING' && { title: 'supervisord can clear the logs of a running process only' }),
    }),
    item('copy-pid', 'Copy PID', '', !(p.PID > 0)),
  ]
}

/** Distinct group names of a config's processes, in order. */
export function configGroups(c: ConfigView): string[] {
  return [...new Set((c.Processes ?? []).map((p) => p.Group))]
}

/**
 * Menu for a whole config file. The grey commands name the file's actual
 * groups, which can differ from the file name when it has a [group:x] section.
 */
export function configMenu(c: ConfigView): MenuItem[] {
  const groups = configGroups(c)
  const star = (groups.length ? groups : ['<group>']).map((g) => `${g}:*`).join(' ')
  const names = (groups.length ? groups : ['<group>']).join(' ')
  return [
    item('start-all', 'Start all in file', `supervisorctl start ${star}`),
    item('stop-all', 'Stop all in file', `supervisorctl stop ${star}`),
    item('restart-all', 'Restart all in file', `supervisorctl restart ${star}`),
    item('open-config', 'Open / edit config', ''),
    item('describe', 'Describe', ''),
    item('reveal', 'Reveal in Finder', ''),
    item('open-log', 'Open log in editor', ''),
    item('apply', 'Apply changes', `supervisorctl update ${names}`, !c.Changed),
    item('remove', 'Remove from supervisor', `supervisorctl remove ${names}`, false, {
      danger: true,
      confirm:
        'Visor stops this file’s processes and removes them from supervisord. The file is not deleted: it moves to Visor’s backups, and you can restore it from there.',
    }),
  ]
}

export function serverMenu(): MenuItem[] {
  return [
    item('check', 'Check for changes', 'supervisorctl reread'),
    item('apply-all', 'Apply all changes', 'supervisorctl update'),
    item('log', 'View supervisord log', 'supervisorctl maintail'),
    item('restart', 'Restart supervisord', 'supervisorctl reload', false, {
      danger: true,
      confirm: 'Restarting supervisord stops every process and starts them again from the config files.',
    }),
    item('shutdown', 'Shut down supervisord', 'supervisorctl shutdown', false, {
      danger: true,
      confirm: 'Shutting down supervisord stops every process. Visor cannot start it again; you start it from a terminal.',
    }),
    item('settings', 'Server settings', ''),
  ]
}

export interface BulkConfirm { title: string; message: string; confirmLabel: string }

/**
 * What to ask before Stop all / Restart all, for every process or (with file)
 * the processes of one config; null when no confirmation is needed.
 */
export function bulkConfirm(what: 'start' | 'stop' | 'restart', processes: number, file?: string): BulkConfirm | null {
  const n = `${processes} ${processes === 1 ? 'process' : 'processes'}`
  const scope = file ? ` in ${file}` : ''
  const whose = file ? `all ${n} in ${file}` : `all ${n} supervisord runs`
  switch (what) {
    case 'stop':
      return { title: `Stop all processes${scope}?`, message: `This stops ${whose}. They stay stopped until you start them again.`, confirmLabel: 'Stop all' }
    case 'restart':
      return { title: `Restart all processes${scope}?`, message: `This stops and starts ${whose}. Work in progress is interrupted.`, confirmLabel: 'Restart all' }
  }
  return null
}

/** Disables items whose feature has not shipped yet, with an explaining tooltip. */
export function comingSoon(items: MenuItem[], ids: string[]): MenuItem[] {
  return items.map((i) => (ids.includes(i.id) ? { ...i, disabled: true, title: 'Coming in this release' } : i))
}
