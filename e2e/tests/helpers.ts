import http from 'node:http'
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

/**
 * Asks the sandbox supervisord directly (XML-RPC over its unix socket) for
 * every process's state, so a test can check what really happened, not just
 * what the dashboard shows.
 */
export async function supervisorStates(): Promise<Record<string, string>> {
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
  const field = (s: string, name: string) => s.match(new RegExp(`<name>${name}</name>\\s*<value><string>([^<]*)</string>`))?.[1]
  const states: Record<string, string> = {}
  for (const s of xml.split('<struct>').slice(1)) {
    const group = field(s, 'group')
    const name = field(s, 'name')
    const state = field(s, 'statename')
    if (name && state) states[`${group}:${name}`] = state
  }
  return states
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
