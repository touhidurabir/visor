import { spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { expect, type Locator, type Page } from '@playwright/test'

/** Fails the test on console errors and uncaught page errors. Call before goto. */
export function collectErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`)
  })
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  return errors
}

/** Opens a hash route and waits until the dashboard has live data. */
export async function openApp(page: Page, route = '/') {
  await page.goto(`/#${route}`)
}

/** The dashboard is loaded and connected to a supervisord 4.x. */
export async function waitForDashboard(page: Page) {
  await openApp(page, '/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()
  await expect(page.getByRole('link', { name: 'reports.ini', exact: true })).toBeVisible()
}

/** The row of a process on the dashboard (by its menu button's label). */
export function processRow(page: Page, fullName: string): Locator {
  return page.locator('.row.proc').filter({ has: page.getByRole('button', { name: `Actions for ${fullName}` }) })
}

/** Waits until a process shows the given state on the dashboard, as its word reads there ("Running"). */
export async function expectState(page: Page, fullName: string, state: string | RegExp, timeout = 15_000) {
  await expect(processRow(page, fullName)).toContainText(state, { timeout })
}

/** Opens a process's ⋯ menu and clicks an item. */
export async function processAction(page: Page, fullName: string, item: string) {
  await page.getByRole('button', { name: `Actions for ${fullName}` }).click()
  await page.getByRole('menuitem', { name: new RegExp(`^${item}`) }).click()
}

/** Calls the sandbox supervisord directly (XML-RPC over its unix socket), as supervisorctl would: outside Visor. */
export async function supervisorCall(method: string, ...params: string[]): Promise<string> {
  const args = params.map((p) => `<param><value><string>${p}</string></value></param>`).join('')
  const body = `<?xml version="1.0"?><methodCall><methodName>${method}</methodName><params>${args}</params></methodCall>`
  return new Promise<string>((resolve, reject) => {
    const req = http.request(
      { socketPath: '/tmp/visor-e2e/s.sock', path: '/RPC2', method: 'POST', headers: { 'Content-Type': 'text/xml', 'Content-Length': Buffer.byteLength(body) } },
      (res) => {
        let data = ''
        res.setEncoding('utf8')
        res.on('data', (c) => (data += c))
        res.on('end', () => resolve(data))
      },
    )
    req.on('error', reject)
    req.end(body)
  })
}

/** Every process's info from the sandbox supervisord (XML-RPC over its unix socket), as raw <struct> bodies. */
async function allProcessInfo(): Promise<string[]> {
  const body = '<?xml version="1.0"?><methodCall><methodName>supervisor.getAllProcessInfo</methodName></methodCall>'
  const xml = await new Promise<string>((resolve, reject) => {
    const req = http.request(
      { socketPath: '/tmp/visor-e2e/s.sock', path: '/RPC2', method: 'POST', headers: { 'Content-Type': 'text/xml', 'Content-Length': Buffer.byteLength(body) } },
      (res) => {
        let data = ''
        res.setEncoding('utf8')
        res.on('data', (c) => (data += c))
        res.on('end', () => resolve(data))
      },
    )
    req.on('error', reject)
    req.end(body)
  })
  return xml.split('<struct>').slice(1)
}

const field = (s: string, name: string, type = 'string') => s.match(new RegExp(`<name>${name}</name>\\s*<value><${type}>([^<]*)</${type}>`))?.[1]

/** Each process's "group:name", from a struct. */
const fullName = (s: string) => {
  const group = field(s, 'group')
  const name = field(s, 'name')
  return group && name ? `${group}:${name}` : ''
}

/**
 * Asks the sandbox supervisord directly (XML-RPC over its unix socket) for
 * every process's state, so a test can check what really happened, not just
 * what the dashboard shows.
 */
export async function supervisorStates(): Promise<Record<string, string>> {
  const states: Record<string, string> = {}
  for (const s of await allProcessInfo()) {
    const state = field(s, 'statename')
    if (fullName(s) && state) states[fullName(s)] = state
  }
  return states
}

/** Every process's PID from the sandbox supervisord (0 when not running), to tell a restart happened. */
export async function supervisorPids(): Promise<Record<string, number>> {
  const pids: Record<string, number> = {}
  for (const s of await allProcessInfo()) if (fullName(s)) pids[fullName(s)] = Number(field(s, 'pid', 'int') ?? 0)
  return pids
}

/** The config row of a file on the dashboard. */
export function configRow(page: Page, fileName: string): Locator {
  return page.locator('.cfg-row').filter({ has: page.getByRole('link', { name: fileName, exact: true }) })
}

/**
 * Takes a config file out of supervisor through the dashboard's menu
 * ("Remove from supervisor": stops its programs, moves the file into Visor's
 * backups, removes its groups). Specs that create a config call this to leave
 * the sandbox as they found it. Does nothing when the file is already gone.
 */
export async function removeConfig(page: Page, path: string) {
  const { existsSync } = await import('node:fs')
  if (!existsSync(path)) return
  const fileName = path.split('/').pop()!
  await openApp(page, '/')
  await expect(configRow(page, fileName)).toBeVisible()
  await configRow(page, fileName).getByRole('button', { name: `Actions for ${fileName}` }).click()
  await page.getByRole('menuitem', { name: /^Remove from supervisor/ }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Remove from supervisor' }).click()
  await expect.poll(() => existsSync(path), { timeout: 20_000 }).toBe(false)
}

/**
 * Writes a sandbox config back to its original content and, when that
 * leaves supervisord running something else (a test that failed after
 * applying an edit), applies it from the dashboard's banner, so later specs
 * see no "changed on disk" banner. With `always`, it looks for the banner
 * even when the file already had its original content.
 */
export async function restoreConfig(page: Page, path: string, original: string, always = false) {
  const { readFileSync, writeFileSync } = await import('node:fs')
  const changed = readFileSync(path, 'utf8') !== original
  if (changed) writeFileSync(path, original)
  if (!changed && !always) return
  await waitForDashboard(page)
  const banner = page.getByRole('status').filter({ hasText: 'changed on disk' })
  try {
    await banner.waitFor({ timeout: 3_000 })
  } catch {
    return
  }
  await banner.getByRole('button', { name: 'Apply changes' }).click()
  // A comment-only difference (or a banner from just before the write) has
  // nothing supervisord would apply: no confirmation then.
  const confirm = page.getByRole('alertdialog').getByRole('button', { name: 'Apply changes' })
  const nothing = page.getByRole('status').filter({ hasText: 'Nothing to apply' })
  await expect(confirm.or(nothing)).toBeVisible()
  if (await confirm.isVisible()) await confirm.click()
  await expect(banner).toBeHidden()
}

/**
 * The words that depend on the OS (frontend/src/lib/platform.ts). The shared
 * Visor runs with VISOR_PLATFORM=linux (playwright.config.ts), so specs use
 * WORDS.linux; platform.spec.ts starts a macOS one to check WORDS.darwin.
 */
export const WORDS = {
  linux: {
    mod: 'Control',
    newConfigHint: 'Ctrl+N',
    findHint: 'Ctrl+F',
    reveal: 'Show in file manager',
    savedPassword: 'Saved in your keyring',
    keptPassword: /Kept in your keyring/,
    countSwitch: 'Show the unread count on the tray icon',
    loginUnsupported: /the package or the AppImage/,
    noBanners: 'This build can’t show desktop banners',
    ready: 'Your computer is ready',
  },
  darwin: {
    mod: 'Meta',
    newConfigHint: '⌘N',
    findHint: '⌘F',
    reveal: 'Reveal in Finder',
    savedPassword: 'Saved in your Keychain',
    keptPassword: /Kept in your Keychain/,
    countSwitch: 'Show the unread count in the menu bar',
    loginUnsupported: /installed Visor\.app/,
    noBanners: 'This build can’t show macOS banners',
    ready: 'Your Mac is ready',
  },
} as const
export type Platform = keyof typeof WORDS
export const words = WORDS.linux

const ROOT = path.resolve(import.meta.dirname, '../..')
const BIN = path.join(ROOT, 'bin/visor-e2e')
export const SANDBOX = '/tmp/visor-e2e'

export interface Visor {
  url: string
  stop: () => Promise<void>
}

/**
 * Starts a server-mode Visor of its own beside the shared one, on a fresh
 * state folder under the e2e sandbox (which serve.sh removes); stop() ends
 * it by its own process handle.
 */
export async function startVisor(name: string, port: number, config: string, onboarded = false, platform: Platform = 'linux'): Promise<Visor> {
  const dir = path.join(SANDBOX, `doctor-${name}`)
  fs.rmSync(dir, { recursive: true, force: true })
  fs.mkdirSync(path.join(dir, 'state'), { recursive: true })
  if (onboarded) fs.writeFileSync(path.join(dir, 'state', 'settings.json'), '{"onboarded":true}\n')
  const child: ChildProcess = spawn(BIN, [], {
    env: {
      ...process.env,
      VISOR_CONFIG: config,
      VISOR_STATE_DIR: path.join(dir, 'state'),
      WAILS_SERVER_HOST: '127.0.0.1',
      WAILS_SERVER_PORT: String(port),
      VISOR_PLATFORM: platform,
    },
    stdio: 'ignore',
  })
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
  const url = `http://127.0.0.1:${port}`
  await expect(async () => {
    const res = await fetch(url)
    expect(res.ok).toBe(true)
  }).toPass({ timeout: 20_000 })
  return {
    url,
    stop: async () => {
      child.kill('SIGTERM')
      await exited
    },
  }
}

/**
 * A config whose supervisord isn't running (nothing listens on its socket).
 * Start supervisord in Doctor really starts it, as a daemon in this folder;
 * stopSupervisord stops it by the PID in its pid file. `extra` is added to
 * the config (e.g. a broken program, so it fails to start).
 */
export function downConfig(name = 'doctor-down-conf', extra = ''): string {
  const dir = path.join(SANDBOX, name)
  fs.rmSync(dir, { recursive: true, force: true })
  fs.mkdirSync(path.join(dir, 'conf.d'), { recursive: true })
  const conf = path.join(dir, 'supervisord.conf')
  fs.writeFileSync(
    conf,
    [
      '[unix_http_server]',
      `file=${dir}/s.sock`,
      '',
      '[supervisord]',
      `pidfile=${dir}/supervisord.pid`,
      `logfile=${dir}/supervisord.log`,
      `childlogdir=${dir}`,
      '',
      '[rpcinterface:supervisor]',
      'supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface',
      '',
      '[include]',
      `files=${dir}/conf.d/*.ini`,
      extra,
    ].join('\n'),
  )
  return conf
}

/** Stops a supervisord Doctor started, by its own PID, and waits for it to go. */
export async function stopSupervisord(conf: string) {
  const pidFile = path.join(path.dirname(conf), 'supervisord.pid')
  if (!fs.existsSync(pidFile)) return
  const pid = Number(fs.readFileSync(pidFile, 'utf8').trim())
  if (!pid) return
  try {
    process.kill(pid, 'SIGTERM')
  } catch {
    return // already gone
  }
  await expect(() => {
    let alive = true
    try {
      process.kill(pid, 0)
    } catch {
      alive = false
    }
    expect(alive).toBe(false)
  }).toPass({ timeout: 15_000 })
}
