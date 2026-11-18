// Action menus as pure data. Each item's `cmd` is the supervisorctl
// equivalent, shown in grey so people can learn it. Wiring items to service
// calls happens in the views that own them; ActionMenu only emits the id.
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { revealLabel } from './platform'

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

/**
 * The shortest name supervisorctl accepts for a process: the bare name when
 * it is the only process of a group of the same name, else group:name.
 */
export function ctlName(p: ProcessView, groupSize: number): string {
  return p.Group === p.Name && groupSize === 1 ? p.Name : p.FullName
}

/** groupSize is how many processes share p's group (see ctlName). */
export function processMenu(p: ProcessView, groupSize: number): MenuItem[] {
  const name = ctlName(p, groupSize)
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

/** Distinct group names of a config's processes, then of the programs supervisord hasn't loaded, in order. */
export function configGroups(c: ConfigView): string[] {
  return [...new Set([...(c.Processes ?? []).map((p) => p.Group), ...(c.NotLoaded ?? []).map((n) => n.Group)])]
}

/** Why Stop and Restart are off for a file supervisord hasn't loaded at all. */
export const NOT_LOADED_REASON = 'supervisord hasn’t loaded this file yet: Load it first'

/**
 * Menu for a whole config file. The grey commands name the file's actual
 * groups, which can differ from the file name when it has a [group:x] section.
 */
export function configMenu(c: ConfigView): MenuItem[] {
  const groups = configGroups(c)
  const star = (groups.length ? groups : ['<group>']).map((g) => `${g}:*`).join(' ')
  const names = (groups.length ? groups : ['<group>']).join(' ')
  // Programs supervisord doesn't know yet: Start loads them first (as
  // update would), and Load adds just them.
  const unloaded = [...new Set((c.NotLoaded ?? []).map((n) => n.Group))]
  const none = unloaded.length > 0 && !(c.Processes ?? []).length
  const off = { title: NOT_LOADED_REASON }
  return [
    unloaded.length
      ? item('start-all', 'Load and start all in file', `supervisorctl update ${unloaded.join(' ')} && supervisorctl start ${star}`)
      : item('start-all', 'Start all in file', `supervisorctl start ${star}`),
    item('stop-all', 'Stop all in file', `supervisorctl stop ${star}`, none, none ? off : {}),
    item('restart-all', 'Restart all in file', `supervisorctl restart ${star}`, none, none ? off : {}),
    item('open-config', 'Open / edit config', ''),
    item('describe', 'Describe', ''),
    item('reveal', revealLabel(), ''),
    item('open-log', 'Open log in editor', ''),
    ...(unloaded.length ? [item('load', 'Load into supervisord', `supervisorctl update ${unloaded.join(' ')}`)] : []),
    // Nothing in the file is loaded: Load is the way in, Apply changes would do the same.
    item('apply', 'Apply changes', `supervisorctl update ${names}`, none || !(c.Changed || c.Uncertain), none ? { title: 'supervisord hasn’t loaded this file yet: use Load' } : {}),
    item('remove', 'Remove from supervisor', `supervisorctl remove ${names}`, false, {
      danger: true,
      confirm:
        'Visor stops this file’s processes and removes them from supervisord. The file is not deleted: it moves to Visor’s backups folder. “Undo” right after, or “Restore” under Recently removed on the dashboard, brings it back.',
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
      confirm: 'Shutting down supervisord stops every process. You can start it again from Doctor.',
    }),
    item('settings', 'Server settings', ''),
  ]
}

/**
 * Menu ids that only work through supervisord (start, stop, apply, …); the
 * rest act on files on disk or open a page, so they work without it.
 */
const NEEDS_SUPERVISORD = new Set(['start', 'stop', 'restart', 'signal', 'clear', 'start-all', 'stop-all', 'restart-all', 'load', 'apply', 'remove', 'check', 'apply-all', 'shutdown'])

/** The menu while supervisord isn't connected: its actions disabled, with the reason as their tooltip. */
export function withoutSupervisord(items: MenuItem[], reason: string): MenuItem[] {
  return items.map((i) => (NEEDS_SUPERVISORD.has(i.id) ? { ...i, disabled: true, title: reason } : i))
}

export interface BulkConfirm { title: string; message: string; confirmLabel: string }

/**
 * What to ask before Stop all / Restart all, for every process or (with file)
 * the processes of one config, naming the server on a server tab; null when
 * no confirmation is needed.
 */
export function bulkConfirm(what: 'start' | 'stop' | 'restart', processes: number, file?: string, server?: string): BulkConfirm | null {
  const n = `${processes} ${processes === 1 ? 'process' : 'processes'}`
  const scope = (file ? ` in ${file}` : '') + (server ? ` on ${server}` : '')
  const whose = file ? `all ${n} in ${file}` : `all ${n} supervisord runs`
  switch (what) {
    case 'stop':
      return { title: `Stop all processes${scope}?`, message: `This stops ${whose}. They stay stopped until you start them again.`, confirmLabel: 'Stop all' }
    case 'restart':
      return { title: `Restart all processes${scope}?`, message: `This stops and starts ${whose}. Work in progress is interrupted.`, confirmLabel: 'Restart all' }
  }
  return null
}

/** What a menu needs to know about the tab's machine. */
export interface HostRules {
  remote: boolean
  readOnly: boolean
}

/** Why config changes are off on a server (M7: configs there are read only). */
export const SERVER_CONFIGS_READ_ONLY = 'Configs on servers are read only in this version of Visor'

const WRITES_CONFIG = new Set(['load', 'apply', 'remove', 'apply-all'])

/**
 * forHost adapts a menu to the tab: on a server, files open as a Download
 * (no Finder or editor here), config changes are off with the reason, and
 * with Read only every action that changes something is gone.
 */
export function forHost(items: MenuItem[], h: HostRules | null | undefined): MenuItem[] {
  if (!h?.remote) return items
  let out = items.flatMap((i): MenuItem[] => {
    if (i.id === 'reveal') return [item('download', 'Download a copy', '')]
    if (i.id === 'open-log') return []
    if (i.id === 'open-config') return [{ ...i, label: 'Open config' }]
    if (WRITES_CONFIG.has(i.id)) return [{ ...i, disabled: true, title: SERVER_CONFIGS_READ_ONLY }]
    return [i]
  })
  if (h.readOnly) out = out.filter((i) => !NEEDS_SUPERVISORD.has(i.id) && i.id !== 'restart')
  return out
}
