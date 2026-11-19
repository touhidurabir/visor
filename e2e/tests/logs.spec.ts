import { appendFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { SANDBOX, openApp, supervisorCall } from './helpers'

// These append lines to shop-scheduler's stdout log (supervisord keeps
// writing "tick" to it) and turn whole-file search on and off in Settings.

const LOGS_PAGE = `/config/${encodeURIComponent(`${SANDBOX}/conf.d/shop-api.ini`)}?tab=logs&process=${encodeURIComponent('shop-scheduler:shop-scheduler')}`

async function stdoutLog(): Promise<string> {
  const xml = await supervisorCall('supervisor.getProcessInfo', 'shop-scheduler:shop-scheduler')
  const path = xml.match(/<name>stdout_logfile<\/name>\s*<value><string>([^<]*)<\/string>/)?.[1]
  if (!path) throw new Error(`no stdout_logfile in ${xml}`)
  return path
}

async function setWholeSearch(page: Page, on: boolean) {
  await openApp(page, '/settings')
  const sw = page.getByRole('switch', { name: 'Search the whole log file' })
  if ((await sw.isChecked()) !== on) await sw.click()
  await expect(sw).toBeChecked({ checked: on })
}

test.describe('logs', () => {
  test.afterEach(async ({ page }) => {
    await setWholeSearch(page, false)
  })

  test('marks levels, filters by them, and searches the whole file only when Settings allows', async ({ page }) => {
    const id = Date.now()
    // An old line, then more than the 64 KiB the Logs tab first loads.
    const filler = Array.from({ length: 1500 }, (_, i) => `filler line ${i} ${'.'.repeat(60)}`).join('\n')
    appendFileSync(
      await stdoutLog(),
      [
        `[2026-11-19 09:00:00] production.ERROR: old-needle-${id} happened`,
        filler,
        `[2026-11-19 10:00:00] production.ERROR: boom-${id}`,
        '[stacktrace]',
        '#0 /app/Foo.php(12): bar()',
        `[2026-11-19 10:00:01] production.WARNING: slow-${id}`,
        `[2026-11-19 10:00:02] production.INFO: fine-${id}`,
        '',
      ].join('\n'),
    )

    await openApp(page, LOGS_PAGE)
    const log = page.getByLabel('stdout log of shop-scheduler')
    await expect(log).toContainText(`fine-${id}`, { timeout: 15_000 })
    await expect(log).not.toContainText(`old-needle-${id}`) // not loaded

    // Level filters: Errors keeps the error entry and its stack trace.
    const levels = page.getByRole('group', { name: 'Show only these levels' })
    await levels.getByRole('button', { name: /^Errors/ }).click()
    await expect(log).toContainText(`boom-${id}`)
    await expect(log).toContainText('#0 /app/Foo.php(12): bar()')
    await expect(log).not.toContainText(`fine-${id}`)
    await expect(log).not.toContainText('filler line')
    await levels.getByRole('button', { name: /^Errors/ }).click()
    await expect(log).toContainText(`fine-${id}`)

    // Off (the default): the search covers the loaded lines and says how to search all of it.
    const search = page.getByRole('searchbox', { name: 'Search log' })
    await search.fill(`old-needle-${id}`)
    await expect(page.getByTestId('search-scope')).toContainText('Searching only the')
    await expect(page.getByTestId('search-scope').getByRole('link', { name: 'Search the whole log file' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Search the whole file' })).toHaveCount(0)

    // On: Search the whole file finds the old line.
    await setWholeSearch(page, true)
    await openApp(page, LOGS_PAGE)
    await expect(page.getByLabel('stdout log of shop-scheduler')).toContainText(`fine-${id}`, { timeout: 15_000 })
    await page.getByRole('searchbox', { name: 'Search log' }).fill(`old-needle-${id}`)
    await page.getByRole('button', { name: 'Search the whole file' }).click()
    const results = page.getByRole('region', { name: 'Whole-file search results' })
    await expect(results.getByRole('status')).toHaveText(/^1 match in the whole file \(/)
    await expect(results).toContainText(`production.ERROR: old-needle-${id} happened`)
    await results.getByRole('button', { name: 'Back to the live log' }).click()
    await expect(page.getByLabel('stdout log of shop-scheduler')).toBeVisible()
  })
})
