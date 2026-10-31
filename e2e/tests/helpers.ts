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

/** Waits until a process shows the given supervisord state on the dashboard. */
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
