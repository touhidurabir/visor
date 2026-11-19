// Doctor's words (board 1 · First launch / setup): the headline and lede
// for each situation, the lens text, and the commands it suggests. The view
// (views/Doctor.vue) only lays them out.
import type { DoctorView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

export type Situation = 'ready' | 'noconf' | 'nosup' | 'down' | 'denied' | 'auth' | 'notwritable'

const copy: Record<Situation, [headline: string, lede: string]> = {
  ready: ['Your Mac is ready', 'Visor found supervisord and everything it runs. From here you can start, stop and watch every process.'],
  noconf: ['Where is your config?', 'supervisord is installed, but its config file is not in any of the usual places. Point Visor to it once and it will remember.'],
  nosup: ['supervisord isn’t installed yet', 'Visor manages supervisord, so it needs to be on this Mac first. Homebrew installs it in about a minute.'],
  down: ['supervisord isn’t running', 'Everything is in place, but supervisord itself is stopped, so there is nothing to connect to yet.'],
  denied: ['supervisord won’t let Visor in', 'supervisord is running, but its socket belongs to root, so your user can’t connect to it. This usually means it was started with sudo.'],
  auth: ['supervisord wants a password', 'supervisord is running, but it turned down the username and password Visor used. Enter the ones from your supervisord.conf in Settings, then check again.'],
  notwritable: ['Visor can watch, but not edit', 'supervisord is running and Visor can control it. The folder your programs live in isn’t writable for you, so adding and editing configs is off.'],
}

// Where Linux says it differently: no Mac, no Homebrew, and a root socket
// that comes from the system service, not from sudo.
const linuxCopy: Partial<Record<Situation, [headline: string, lede: string]>> = {
  ready: ['Your computer is ready', copy.ready[1]],
  nosup: ['supervisord isn’t installed yet', 'Visor manages supervisord, so it needs to be on this computer first. Your package manager installs it in about a minute.'],
  denied: ['supervisord won’t let Visor in', 'supervisord runs as a system service, and its socket belongs to root, so your user can’t connect to it. Letting a group use the socket fixes it.'],
}

/** The headline and lede for a situation; anything unknown reads as ready. */
export function situationCopy(s: string, mac = true): { headline: string; lede: string } {
  const [headline, lede] = (!mac && linuxCopy[s as Situation]) || copy[s as Situation] || copy.ready
  return { headline, lede }
}

/** The lens centre: a tick and "5 of 5 checks passed", or "!" and how many to fix. */
export function lensText(v: Pick<DoctorView, 'Steps'>): { big: string; small: string } {
  const steps = v.Steps ?? []
  const bad = steps.filter((s) => s.Status === 'bad').length
  if (bad === 0) return { big: '✓', small: `${steps.length} of ${steps.length} checks passed` }
  return { big: '!', small: `${bad} ${bad === 1 ? 'thing' : 'things'} to fix` }
}

/**
 * stepLook is how a check shows its status: the mark, the word beside it
 * (board 1: in words, not by glyph and colour alone) and the badge tone.
 * Anything unknown reads as waiting.
 */
export function stepLook(status: string): { glyph: string; word: string; tone: 'ok' | 'bad' | 'idle' } {
  switch (status) {
    case 'ok':
      return { glyph: '✓', word: 'Done', tone: 'ok' }
    case 'bad':
      return { glyph: '!', word: 'Needs attention', tone: 'bad' }
  }
  return { glyph: '', word: 'Waiting', tone: 'idle' }
}

export const installCommands = ['brew install supervisor', 'brew services start supervisor']

/** The commands that install and start supervisord: Homebrew's on macOS, the distro's on Linux. */
export function installCommandsFor(v: Pick<DoctorView, 'Linux'> | null): string[] {
  const l = v?.Linux
  if (!l) return installCommands
  if (!l.Install.startsWith('sudo ')) return [l.Install]
  return [l.Install, `sudo systemctl enable --now ${l.Service}`]
}
export const startCommand = 'brew services start supervisor'

/** What usually stops supervisord from starting, shown under its output. */
export const startTips = [
  'A config mistake: supervisord names the file and line it can’t use, above.',
  'Another supervisord already uses the socket or port: stop that one, or check with Check again whether it’s the one you want.',
  'A socket or pid file left from a crash: if nothing is running, delete the file the log names and start again.',
  'It was started with sudo before: see “supervisord runs as root?” below.',
]

/** startTips for this OS: on Linux the last one is about the system service. */
export function startTipsFor(mac: boolean): string[] {
  if (mac) return startTips
  return [
    ...startTips.slice(0, 3),
    'The system’s supervisord (sudo systemctl status supervisor or supervisord) already uses that socket: stop it, or point Visor at its config.',
  ]
}

/** The command that gives this user the config folder ("" when it isn't known). */
export function chownCommand(dir: string): string {
  return dir ? `sudo chown -R $(whoami) ${shellQuote(dir)}` : ''
}

/** The command that creates a config folder that isn't there yet. */
export function mkdirCommand(dir: string): string {
  return dir ? `mkdir -p ${shellQuote(dir)}` : ''
}

function shellQuote(s: string): string {
  return /^[\w./-]+$/.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`
}

export interface Fix {
  title: string
  why: string
  commands: string[]
}

/** The three ways out of a root-owned socket (brief §7), the first one open. */
export function rootFixes(user: string): Fix[] {
  return [
    {
      title: 'Run supervisord as yourself (recommended)',
      why: 'Stop the copy root started and start it again as your user. The socket is then yours and nothing else changes.',
      commands: ['sudo brew services stop supervisor', 'brew services start supervisor'],
    },
    {
      title: 'Open the socket to your user',
      why: 'Keep supervisord as root but let your user use the socket. Add these lines under [unix_http_server] in supervisord.conf, then restart supervisord.',
      commands: [`chown=${user || 'you'}:staff`, 'chmod=0770'],
    },
    {
      title: 'Use a local port with a password',
      why: 'Have supervisord listen on this Mac only, with a username and password. Add an [inet_http_server] section, restart supervisord, and enter the password in Settings.',
      commands: ['[inet_http_server]\nport=127.0.0.1:9001\nusername=visor\npassword=choose-one'],
    },
  ]
}

/**
 * reportLook is stepLook for a server report's row (ok, error, warning,
 * info, skipped), and the lens segment it lights.
 */
export function reportLook(status: string): { glyph: string; word: string; tone: 'ok' | 'bad' | 'warn' | 'idle'; segment: 'ok' | 'bad' | 'wait' } {
  switch (status) {
    case 'ok':
      return { glyph: '✓', word: 'Done', tone: 'ok', segment: 'ok' }
    case 'error':
      return { glyph: '!', word: 'Needs attention', tone: 'bad', segment: 'bad' }
    case 'warning':
      return { glyph: '!', word: 'Worth a look', tone: 'warn', segment: 'wait' }
    case 'info':
      return { glyph: 'i', word: 'Note', tone: 'idle', segment: 'wait' }
  }
  return { glyph: '', word: 'Skipped', tone: 'idle', segment: 'wait' }
}

/** The lens centre for a server report: a tick, or "!" and how many rows to fix. */
export function reportLens(rows: { Status: string }[]): { big: string; small: string } {
  const bad = rows.filter((r) => r.Status === 'error').length
  if (bad === 0) {
    const ok = rows.filter((r) => r.Status === 'ok').length
    return { big: '✓', small: `${ok} ${ok === 1 ? 'check' : 'checks'} passed` }
  }
  return { big: '!', small: `${bad} ${bad === 1 ? 'thing' : 'things'} to fix` }
}
