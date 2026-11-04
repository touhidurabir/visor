import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, downConfig, startVisor, waitForDashboard } from './helpers'

/** A process's row in the popover, in a group when given. */
function trayRow(page: Page, name: string, group?: string) {
  const scope = group ? page.getByRole('region', { name: group, exact: true }) : page
  return scope.getByTestId('tray-row').filter({ has: page.getByText(name, { exact: true }) })
}

test.describe('menu-bar popover', () => {
  test('the bell counts unread notifications; there is no list of them', async ({ page }) => {
    test.setTimeout(90_000)
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    await expect(page.getByText(/\d+ failing/)).toBeVisible()
    // mailer-digest fails at start and flaky exits every 20 s, so something
    // is unread soon.
    await expect(page.getByTestId('tray-bell')).toHaveAccessibleName(/Notifications, \d+ unread/, { timeout: 45_000 })
    await expect(page.getByText('New since you last looked')).toHaveCount(0)
    expect(errors).toEqual([])
  })

  test('failing processes come first, in Needs attention; the rest follow by config', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const attention = page.getByRole('region', { name: 'Needs attention' })
    await expect(page.getByRole('region').first()).toHaveAccessibleName('Needs attention')
    // mailer-digest exits at once and reports-sync has no binary: both fail.
    await expect(trayRow(page, 'mailer-digest', 'Needs attention')).toBeVisible()
    await expect(trayRow(page, 'reports-sync', 'Needs attention')).toBeVisible()
    for (const t of await attention.getByTestId('tray-time').allTextContents()) expect(t).toMatch(/^failing/)
    // Their configs list the rest, not them again.
    await expect(trayRow(page, 'mailer-worker_00', 'mailer.ini')).toBeVisible()
    await expect(trayRow(page, 'mailer-digest', 'mailer.ini')).toHaveCount(0)
    await expect(trayRow(page, 'reports-sync', 'reports.ini')).toHaveCount(0)
    await expect(trayRow(page, 'shop-scheduler', 'shop-api.ini')).toBeVisible()
    expect(errors).toEqual([])
  })

  test('each row shows only the actions that apply, and times say what they mean', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')

    const running = trayRow(page, 'shop-scheduler')
    await expect(running.getByTestId('tray-time')).toHaveText(/^up \d+[smhd]/)
    await expect(running.getByRole('button', { name: 'Stop shop-scheduler', exact: true })).toBeVisible()
    await expect(running.getByRole('button', { name: 'Restart shop-scheduler', exact: true })).toBeVisible()
    await expect(running.getByRole('button', { name: 'Start shop-scheduler', exact: true })).toHaveCount(0)

    const failed = trayRow(page, 'reports-sync')
    await expect(failed.getByTestId('tray-time')).toHaveText(/^failing \d+[smhd]/)
    await expect(failed.getByRole('button')).toHaveCount(1)
    await expect(failed.getByRole('button', { name: 'Start reports-sync', exact: true })).toBeVisible()

    // notes-indexer runs for 2 s and exits as expected: stopped.
    const stopped = trayRow(page, 'notes-indexer')
    await expect(stopped.getByTestId('tray-time')).toHaveText(/^(stopped \d+[smhd]|exited)$/, { timeout: 15_000 })
    await expect(stopped.getByRole('button')).toHaveCount(1)
    await expect(stopped.getByRole('button', { name: 'Start notes-indexer', exact: true })).toBeVisible()

    // Every time has its word, never a bare "4m".
    for (const t of await page.getByTestId('tray-time').allTextContents()) expect(t).toMatch(/^(up|failing|stopped) \d|^[a-z]+$/)
    expect(errors).toEqual([])
  })

  test('the footer has Restart all, Apply config changes and Open Visor…', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const foot = page.getByRole('contentinfo')
    await expect(foot.getByRole('button')).toHaveText(['Restart all', 'Apply config changes', 'Open Visor…'])
    await expect(foot.getByRole('button', { name: 'Restart all', exact: true })).toBeEnabled()
    await expect(foot.getByRole('button', { name: 'Apply config changes' })).toBeEnabled()
    expect(errors).toEqual([])
  })

  test('each config shows whether it is watched and switches it', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const sw = page.getByRole('button', { name: 'Watch notes.ini' })
    await expect(sw).toHaveAttribute('aria-pressed', 'true')
    await expect(sw).toHaveText('Watching')

    await sw.click()
    await expect(sw).toHaveAttribute('aria-pressed', 'false')
    await expect(sw).toHaveText('Watch')
    // Saved, not just shown.
    await page.reload()
    await expect(sw).toHaveAttribute('aria-pressed', 'false')

    // Put it back for the other tests.
    await sw.click()
    await expect(sw).toHaveAttribute('aria-pressed', 'true')
    expect(errors).toEqual([])
  })
})

// Its own Visor on a config whose supervisord isn't running, so the shared
// sandbox keeps running.
test.describe('menu-bar popover, supervisord down', () => {
  test('says so plainly, offers Doctor, and the disabled buttons say why', async ({ page, context }) => {
    test.setTimeout(60_000)
    const conf = downConfig('tray-down-conf')
    const dir = path.dirname(conf)
    fs.writeFileSync(path.join(dir, 'conf.d', 'down.ini'), '[program:down-sleeper]\ncommand=/bin/sleep 600\nautostart=false\n')
    const visor = await startVisor('tray-down', 8778, conf, true)
    try {
      const errors = collectErrors(page)
      // The main window, on the dashboard (a failed first connection opens Doctor).
      const main = await context.newPage()
      await main.goto(visor.url + '/#/')
      await expect(main).toHaveURL(/#\/doctor$/, { timeout: 15_000 })
      await main.getByRole('link', { name: 'Dashboard' }).first().click()
      await expect(main).toHaveURL(/#\/$/)

      await page.goto(visor.url + '/#/tray')
      const card = page.getByRole('alert', { name: 'supervisord isn’t running' })
      await expect(card).toBeVisible({ timeout: 15_000 })
      await expect(card).toContainText('Visor can’t show or control processes until it’s back. Doctor walks you through starting it.')
      // The raw Go error only under Show details.
      const raw = card.getByTestId('connection-error')
      await expect(raw).toBeHidden()
      await card.getByText('Show details').click()
      await expect(raw).toContainText(`dial unix ${dir}/s.sock`)
      await expect(page.getByRole('banner')).toContainText('Not running')
      // No process list while there is nothing to list.
      await expect(page.getByRole('region')).toHaveCount(0)

      // The footer's actions are off, and say why.
      const foot = page.getByRole('contentinfo')
      for (const name of ['Restart all', 'Apply config changes']) {
        const b = foot.getByRole('button', { name, exact: true })
        await expect(b).toBeDisabled()
        await expect(b).toHaveAttribute('title', 'supervisord isn’t running')
      }
      await expect(foot.getByTestId('tray-offline')).toHaveText('These need supervisord running.')
      await expect(foot.getByRole('button', { name: 'Open Visor…' })).toBeEnabled()

      // Try again says when nothing changed.
      await card.getByRole('button', { name: 'Try again' }).click()
      await expect(page.getByRole('status').filter({ hasText: 'Still not connected: supervisord isn’t running.' })).toBeVisible({ timeout: 15_000 })

      // Open Doctor takes the main window to Doctor.
      await card.getByRole('button', { name: 'Open Doctor' }).click()
      await expect(main).toHaveURL(/#\/doctor$/, { timeout: 10_000 })
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
    }
  })
})
