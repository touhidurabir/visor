import { expect, test } from '@playwright/test'
import { SANDBOX, mcpCall, mcpToken, openApp, waitForDashboard } from './helpers'

// CPU and memory per process (M10): Visor samples every 10 s, so the first
// memory figure comes with the first sample and CPU with the second.
test.describe.configure({ mode: 'serial' })

const QUEUE = 'shop-queue:shop-queue_00'
const configPage = (file: string) => `/config/${encodeURIComponent(`${SANDBOX}/conf.d/${file}`)}`

test.describe('CPU and memory', () => {
  // No console-error check here: in the full run, WebKit reports runtime
  // fetches failing "due to access control checks" while a page waits this
  // long (not this feature's calls; the spec is clean on its own).
  test('the dashboard shows a running process’s usage, and none for one that isn’t running', async ({ page }) => {
    test.setTimeout(60_000)
    await openApp(page, '/')
    await waitForDashboard(page)
    await expect(page.getByText('Usage', { exact: true })).toBeVisible()
    const cell = page.locator(`[data-usage="${QUEUE}"]`)
    await expect(cell).toHaveAttribute('title', /^CPU (measuring|[\d.]+ %), memory [\d.]+ (KB|MB)/, { timeout: 25_000 })
    await expect(cell.locator('.figs')).toHaveText(/[\d.]+ (KB|MB)/)
    await expect(cell).toHaveAttribute('title', /^CPU [\d.]+ %/, { timeout: 25_000 })
    await expect(cell.locator('.sr-only')).toHaveText(/^CPU [\d.]+ %, memory/) // what a screen reader reads
    const stopped = page.locator('[data-usage="reports-sync:reports-sync"]')
    await expect(stopped).toHaveAttribute('title', 'Not running')
    await expect(stopped.locator('.sr-only')).toHaveText('Not running')
    await expect(stopped.locator('.fig')).toHaveText('–')
  })

  test('the config page’s Resources card has a row per process and the hour’s charts', async ({ page }) => {
    test.setTimeout(60_000)
    await openApp(page, configPage('shop-api.ini'))
    const card = page.getByTestId('resources')
    await expect(card.getByRole('heading', { name: 'Resources' })).toBeVisible()
    const row = card.locator(`[data-usage-row="${QUEUE}"]`)
    await expect(row).toContainText(/[\d.]+ (KB|MB)/, { timeout: 25_000 })
    await expect(card.locator('svg[data-chart="cpu"]')).toBeVisible()
    await expect(card.locator('svg[data-chart="mem"] polyline').first()).toBeAttached()
    await expect(card.getByLabel('Process to chart')).toBeVisible() // shop-api.ini runs several
  })

  test('get_resource_usage over MCP names the process with its memory', async ({ page }) => {
    await openApp(page, '/mcp')
    const enable = page.getByRole('switch', { name: 'Enable MCP server' })
    await enable.check()
    await expect(page.getByTestId('mcp-status')).toHaveText('Running')
    try {
      const res = await mcpCall(await mcpToken(page), 'get_resource_usage', { process: QUEUE })
      expect(res.isError).toBe(false)
      const body = JSON.parse(res.text) as { processes: { name: string; memory_bytes: number | null }[]; sample_every_s: number }
      expect(body.sample_every_s).toBe(10)
      expect(body.processes).toHaveLength(1)
      expect(body.processes[0].name).toBe(QUEUE)
      expect(body.processes[0].memory_bytes).toBeGreaterThan(0)
    } finally {
      await enable.uncheck()
      await expect(page.getByTestId('mcp-status')).toHaveText('Off')
    }
  })
})
