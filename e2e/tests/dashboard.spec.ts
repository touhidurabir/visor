import { expect, test } from '@playwright/test'
import { collectErrors, downConfig, processRow, startVisor, stopSupervisord, waitForDashboard, words } from './helpers'

test.describe('dashboard', () => {
  test('shows the connected server and its config files', async ({ page }) => {
    await waitForDashboard(page)
    await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()
    for (const file of ['shop-api.ini', 'mailer.ini', 'reports.ini', 'notes.ini', 'flaky.ini']) {
      await expect(page.getByRole('link', { name: file, exact: true })).toBeVisible()
    }
  })

  test('the health strip says what is failing and shows non-zero totals', async ({ page }) => {
    await waitForDashboard(page)
    const strip = page.getByRole('region', { name: 'Health' })
    const count = (label: string) => strip.locator('.count').filter({ has: page.locator('dt', { hasText: new RegExp(`^${label}$`) }) }).locator('dd')
    // 5 files, 12 processes (4 + 1 + 2 + 1 + 2 + 1 + 1).
    await expect(count('configs')).toHaveText('5')
    await expect(count('processes')).toHaveText('12')
    await expect(async () => {
      expect(Number(await count('running').innerText())).toBeGreaterThan(0)
    }).toPass()
    // reports-sync can't start and mailer-digest exits at once: both fail
    // (flaky can join them for a moment, between its exit and restart).
    await expect(strip).toContainText(/\d+ processes are failing/, { timeout: 20_000 })
    await expect(strip).toContainText(/mailer-digest.* need attention\./)
    await expect(strip).toContainText(/reports-sync.* need attention\./)
    await expect(strip.getByRole('link', { name: /open issues?$/ })).toHaveAttribute('href', '#/issues')
  })

  test('Show them focuses the first failing row', async ({ page }) => {
    await waitForDashboard(page)
    const strip = page.getByRole('region', { name: 'Health' })
    await expect(strip).toContainText(/\d+ processes are failing/, { timeout: 20_000 })
    // A search that hides the failing rows is cleared first.
    const search = page.getByRole('searchbox', { name: 'Search processes and config files' })
    await search.fill('shop')
    await expect(processRow(page, 'reports-sync:reports-sync')).toBeHidden()
    await strip.getByRole('button', { name: /^Show them/ }).click()
    await expect(search).toHaveValue('')
    const focused = page.locator('.row.proc:focus')
    await expect(focused).toHaveClass(/failing/)
    await expect(focused).toBeInViewport()
  })

  test('a failing process is listed first in its config, tinted, with its full reason', async ({ page }) => {
    await waitForDashboard(page)
    // reports.ini defines reports-export before reports-sync; sync can't start.
    const rows = page.locator('.cfg').filter({ has: page.getByRole('link', { name: 'reports.ini', exact: true }) }).locator('.row.proc')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toContainText('Fatal', { timeout: 20_000 })
    await expect(rows.nth(0).getByRole('link')).toHaveText('reports-sync')
    await expect(rows.nth(1).getByRole('link')).toHaveText('reports-export')
    await expect(rows.nth(0)).toHaveClass(/failing/)
    await expect(rows.nth(1)).not.toHaveClass(/failing/)
    // The reason sits under the name and isn't cut off.
    const why = rows.nth(0).locator('.why')
    await expect(why).not.toBeEmpty()
    expect(await why.evaluate((el) => el.scrollWidth <= el.clientWidth && getComputedStyle(el).textOverflow !== 'ellipsis')).toBe(true)
  })

  test('the Watch buttons keep their words for screen readers and in the tooltip', async ({ page }) => {
    await waitForDashboard(page)
    const watch = page.locator('.cfg-row').filter({ has: page.getByRole('link', { name: 'mailer.ini', exact: true }) }).locator('button[aria-pressed]')
    await expect(watch).toHaveAccessibleName(/^(Watching|Watch) mailer\.ini$/)
    await expect(watch).toHaveAttribute('title', /^(Watching|Watch):/)
  })

  test('search focuses with the shortcut and filters config files', async ({ page }) => {
    await waitForDashboard(page)
    await page.locator('body').click({ position: { x: 5, y: 5 } })
    await page.keyboard.press(`${words.mod}+f`)
    const search = page.getByRole('searchbox', { name: 'Search processes and config files' })
    await expect(search).toBeFocused()
    await search.fill('mailer')
    await expect(page.getByRole('link', { name: 'mailer.ini', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'shop-api.ini', exact: true })).toBeHidden()
    await search.fill('')
    await expect(page.getByRole('link', { name: 'shop-api.ini', exact: true })).toBeVisible()
  })

  test('the table fits the default 1000 px window with the ⋯ column in view', async ({ page }) => {
    await page.setViewportSize({ width: 1000, height: 700 })
    await waitForDashboard(page)
    const frame = page.locator('.frame').first()
    expect(await frame.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(0)
    const box = (await frame.boundingBox())!
    const menu = (await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).boundingBox())!
    expect(menu.x + menu.width).toBeLessThanOrEqual(box.x + box.width)
    // Name is in view too: nothing was scrolled sideways to reach the menu.
    const name = (await page.getByRole('link', { name: 'reports.ini', exact: true }).boundingBox())!
    expect(name.x).toBeGreaterThanOrEqual(box.x)
  })

  test('after Shut down, the toast offers Open Doctor, which opens Doctor', async ({ page }) => {
    test.setTimeout(90_000)
    // Its own supervisord and Visor, so the shared sandbox keeps running.
    const conf = downConfig('dashboard-shutdown-conf')
    const visor = await startVisor('dashboard-shutdown', 8776, conf, true)
    try {
      const errors = collectErrors(page)
      // Nothing runs yet, so Visor opens on Doctor: start supervisord there.
      await page.goto(visor.url + '/#/')
      await page.getByRole('button', { name: 'Start supervisord' }).click({ timeout: 15_000 })
      await expect(page.getByRole('heading', { name: 'Your Mac is ready' })).toBeVisible({ timeout: 20_000 })
      await page.getByRole('button', { name: 'Open dashboard' }).click()
      await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()

      await page.getByRole('button', { name: 'Server actions' }).click()
      await page.getByRole('menuitem', { name: /^Shut down supervisord/ }).click()
      const dialog = page.getByRole('alertdialog')
      await expect(dialog).toContainText('You can start it again from Doctor.')
      await dialog.getByRole('button', { name: 'Shut down supervisord' }).click()
      const toast = page.getByRole('status').filter({ hasText: 'supervisord shut down. Start it again from Doctor.' })
      await expect(toast).toBeVisible()
      await toast.getByRole('button', { name: 'Open Doctor' }).click()
      await expect(page).toHaveURL(/#\/doctor$/)
      await expect(page.getByRole('heading', { name: 'supervisord isn’t running' })).toBeVisible({ timeout: 15_000 })
      // Using the action dismissed the toast.
      await expect(toast).toBeHidden()
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
      await stopSupervisord(conf)
    }
  })
})
