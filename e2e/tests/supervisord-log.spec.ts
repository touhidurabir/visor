import { expect, test } from '@playwright/test'
import { waitForDashboard } from './helpers'

// The supervisord log tab reads the sandbox supervisord's real log, which
// always holds "spawned" INFO lines (and a flaky process's WARN exits).
test.describe('supervisord log tab', () => {
  const lines = (page: import('@playwright/test').Page) => page.locator('.logtab .line')

  test('the server menu opens the tab, which lists spawned lines', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Server actions' }).click()
    await page.getByRole('menuitem', { name: /^View supervisord log/ }).click()
    await expect(page).toHaveURL(/#\/issues\/log/)
    await expect(lines(page).filter({ hasText: 'spawned:' }).first()).toBeVisible()
    await expect(lines(page).first().locator('.lvl')).toBeVisible()
    await expect(page.getByText(/^Showing \d+ (of \d+ matching )?lines?$/)).toBeVisible()
  })

  test('level chips and search narrow the lines', async ({ page }) => {
    await page.goto('/#/issues/log')
    const info = lines(page).filter({ has: page.locator('.lvl.info') })
    await expect(info.first()).toBeVisible()

    // Warnings only: the flaky process exits "not expected" at WARN.
    await page.getByRole('button', { name: 'Info', exact: true }).click()
    await page.getByRole('button', { name: 'Errors', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Warnings', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('button', { name: 'Info', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await expect(page).toHaveURL(/lv=warnings/)
    await expect(info).toHaveCount(0)
    await expect(lines(page).first()).toBeVisible({ timeout: 60_000 })
    await expect(lines(page).filter({ hasNot: page.locator('.lvl.warn') })).toHaveCount(0)

    // Back to info lines, narrowed by search.
    await page.getByRole('button', { name: 'Info', exact: true }).click()
    await page.getByRole('button', { name: 'Warnings', exact: true }).click()
    const search = page.getByRole('searchbox', { name: 'Search the log' })
    await search.fill('spawned: ')
    await expect(page).toHaveURL(/q=spawned/)
    await expect(lines(page).first()).toBeVisible()
    await expect(lines(page).filter({ hasNotText: 'spawned:' })).toHaveCount(0)
    await search.fill('no-such-text-xyz')
    await expect(page.getByText('No log lines match your filters.')).toBeVisible()
  })

  test('the filters survive a reload and picking no level says so', async ({ page }) => {
    await page.goto('/#/issues/log?lv=errors&q=zzz-nothing')
    await expect(page.getByRole('button', { name: 'Errors', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('searchbox', { name: 'Search the log' })).toHaveValue('zzz-nothing')
    await page.getByRole('button', { name: 'Errors', exact: true }).click()
    await expect(page.getByText('Pick at least one level to see lines.')).toBeVisible()
  })

  test('Follow picks up new lines without a click', async ({ page }) => {
    await page.goto('/#/issues/log')
    await expect(lines(page).first()).toBeVisible()
    await page.getByLabel('Follow').check()
    // The flaky process spawns about every 20 s, so the count moves on its own.
    const before = await lines(page).count()
    await expect(async () => {
      expect(await lines(page).count()).toBeGreaterThan(before)
    }).toPass({ timeout: 60_000 })
  })
})
