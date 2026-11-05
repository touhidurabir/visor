import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, downConfig, restoreConfig, startVisor, stopSupervisord, supervisorPids, waitForDashboard } from './helpers'

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
    for (const t of await attention.getByTestId('tray-time').allTextContents()) expect(t).toMatch(/^(Fatal|Backoff|Exited)( · failing \d+[smhd]|$)/)
    // Their configs list the rest, not them again.
    await expect(trayRow(page, 'mailer-worker', 'mailer.ini')).toBeVisible()
    await expect(trayRow(page, 'mailer-digest', 'mailer.ini')).toHaveCount(0)
    await expect(trayRow(page, 'reports-sync', 'reports.ini')).toHaveCount(0)
    await expect(trayRow(page, 'shop-scheduler', 'shop-api.ini')).toBeVisible()
    expect(errors).toEqual([])
  })

  test('each row has two lines and only the word buttons that apply', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')

    const running = trayRow(page, 'shop-scheduler')
    await expect(running.getByTestId('tray-time')).toHaveText(/^Running · up \d+[smhd]/)
    await expect(running.getByRole('button')).toHaveText(['Stop', 'Restart'])
    await expect(running.getByRole('button', { name: 'Stop shop-scheduler', exact: true })).toBeVisible()
    await expect(running.getByRole('button', { name: 'Restart shop-scheduler', exact: true })).toBeVisible()
    await expect(running.getByRole('button', { name: 'Start shop-scheduler', exact: true })).toHaveCount(0)

    const failed = trayRow(page, 'reports-sync')
    await expect(failed.getByTestId('tray-time')).toHaveText(/^Fatal · failing \d+[smhd]/)
    await expect(failed.getByRole('button')).toHaveCount(1)
    await expect(failed.getByRole('button', { name: 'Start reports-sync', exact: true })).toHaveText('Start')

    // notes-indexer runs for 2 s and exits as expected: stopped, its time without "stopped" twice.
    const stopped = trayRow(page, 'notes-indexer')
    await expect(stopped.getByTestId('tray-time')).toHaveText(/^Exited( · \d+[smhd])?$/, { timeout: 15_000 })
    await expect(stopped.getByRole('button')).toHaveCount(1)
    await expect(stopped.getByRole('button', { name: 'Start notes-indexer', exact: true })).toHaveText('Start')

    // Every second line starts with its state (or counts copies); never a bare "4m".
    for (const t of await page.getByTestId('tray-time').allTextContents()) {
      expect(t).toMatch(/^(Running|Starting) · up \d|^[A-Z][a-z]+ · failing \d|^(Stopped|Exited) · \d|^[A-Z][a-z]+$|^\d+ of \d+ running( · \d+ failing)?$/)
    }
    expect(errors).toEqual([])
  })

  test('copies of one program are one ×N row that unfolds; its Restart restarts every copy', async ({ page }) => {
    test.setTimeout(60_000)
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const mailer = page.getByRole('region', { name: 'mailer.ini', exact: true })
    const row = trayRow(page, 'mailer-worker', 'mailer.ini')
    await expect(row).toContainText('×2')
    await expect(row.getByTestId('tray-time')).toHaveText('2 of 2 running')
    const fold = row.getByRole('button', { name: 'Show the 2 copies of mailer-worker' })
    await expect(fold).toHaveAttribute('aria-expanded', 'false')
    await expect(trayRow(page, 'mailer-worker_00', 'mailer.ini')).toHaveCount(0)

    await fold.click()
    await expect(row.getByRole('button', { name: 'Hide the 2 copies of mailer-worker' })).toHaveAttribute('aria-expanded', 'true')
    const copies = mailer.getByRole('group', { name: 'Copies of mailer-worker' })
    for (const n of ['mailer-worker_00', 'mailer-worker_01']) {
      const copy = copies.getByTestId('tray-row').filter({ has: page.getByText(n, { exact: true }) })
      await expect(copy.getByTestId('tray-time')).toHaveText(/^Running · up \d+[smhd]/)
      await expect(copy.getByRole('button')).toHaveText(['Stop', 'Restart'])
      await expect(copy.getByRole('button', { name: `Restart ${n}`, exact: true })).toBeVisible()
    }

    // The row's Restart acts on both copies, one call each.
    const before = await supervisorPids()
    const names = ['mailer-worker:mailer-worker_00', 'mailer-worker:mailer-worker_01']
    for (const n of names) expect(before[n]).toBeGreaterThan(0)
    await row.getByRole('button', { name: 'Restart mailer-worker', exact: true }).click()
    await expect
      .poll(async () => {
        const now = await supervisorPids()
        return names.every((n) => now[n] > 0 && now[n] !== before[n])
      }, { timeout: 20_000 })
      .toBe(true)
    await expect(row.getByTestId('tray-time')).toHaveText('2 of 2 running', { timeout: 15_000 })
    await expect(page.getByRole('alert')).toHaveCount(0)

    await row.getByRole('button', { name: 'Hide the 2 copies of mailer-worker' }).click()
    await expect(copies).toHaveCount(0)
    expect(errors).toEqual([])
  })

  test('the footer has Restart all, Apply changes and Open Visor…', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const foot = page.getByRole('contentinfo')
    await expect(foot.getByRole('button')).toHaveText(['Restart all', 'Apply changes', 'Open Visor…'])
    await expect(foot.getByRole('button', { name: 'Restart all', exact: true })).toBeEnabled()
    const apply = foot.getByRole('button', { name: 'Apply changes', exact: true })
    await expect(apply).toBeEnabled()
    await expect(apply).toHaveAttribute('title', 'Apply config changes')
    expect(errors).toEqual([])
  })

  test('the footer’s Apply changes still runs the apply flow', async ({ page }) => {
    test.setTimeout(60_000)
    const NOTES = '/tmp/visor-e2e/conf.d/notes.ini'
    const original = fs.readFileSync(NOTES, 'utf8')
    const errors = collectErrors(page)
    try {
      await waitForDashboard(page)
      await page.goto('/#/tray')
      const apply = page.getByRole('contentinfo').getByRole('button', { name: 'Apply changes', exact: true })
      await apply.click()
      await expect(page.getByRole('status').filter({ hasText: 'Nothing to apply' })).toBeVisible()

      fs.writeFileSync(NOTES, original.replace('sleep 2', 'sleep 3'))
      await apply.click()
      const dialog = page.getByRole('alertdialog', { name: 'Apply config changes' })
      await expect(dialog).toContainText('Changed: notes-indexer')
      await dialog.getByRole('button', { name: 'Cancel' }).click()
      await expect(dialog).toHaveCount(0)

      await apply.click()
      await dialog.getByRole('button', { name: 'Apply', exact: true }).click()
      await expect(page.getByRole('status').filter({ hasText: 'Applied: 1 restarted' })).toBeVisible({ timeout: 15_000 })
      expect(errors).toEqual([])
    } finally {
      await restoreConfig(page, NOTES, original, true)
    }
  })

  test('each config’s bell shows whether it is watched and switches it', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await page.goto('/#/tray')
    const sw = page.getByRole('region', { name: 'notes.ini', exact: true }).getByRole('button', { name: /^Watch(ing)? notes\.ini$/ })
    await expect(sw).toHaveAttribute('aria-pressed', 'true')
    await expect(sw).toHaveAccessibleName('Watching notes.ini')
    await expect(sw).toHaveAttribute('title', 'Visor notifies you about problems in this file. Click to stop.')
    // An icon, big enough to hit: 30 px.
    await expect(sw).toHaveText('')
    expect(await sw.evaluate((el) => el.getBoundingClientRect().height)).toBe(30)

    await sw.click()
    await expect(sw).toHaveAttribute('aria-pressed', 'false')
    await expect(sw).toHaveAccessibleName('Watch notes.ini')
    await expect(sw).toHaveAttribute('title', 'Visor stays quiet about this file. Click to watch it.')
    // Saved, not just shown.
    await page.reload()
    await expect(sw).toHaveAttribute('aria-pressed', 'false')

    // Put it back for the other tests.
    await sw.click()
    await expect(sw).toHaveAttribute('aria-pressed', 'true')
    await expect(sw).toHaveAccessibleName('Watching notes.ini')
    expect(errors).toEqual([])
  })
})

// Its own Visor and supervisord, so a failing copy doesn't disturb the
// shared sandbox: e2e-copies runs two copies, and the second exits at once.
test.describe('menu-bar popover, a failing copy', () => {
  test('is listed under Needs attention and in its copies row, which turns red', async ({ page }) => {
    test.setTimeout(60_000)
    const conf = downConfig('tray-copies-conf')
    const dir = path.dirname(conf)
    fs.writeFileSync(
      path.join(dir, 'conf.d', 'copies.ini'),
      [
        '[program:e2e-copies]',
        `command=/bin/sh -c '[ %(process_num)d = 1 ] && exit 1; exec /bin/sleep 600'`,
        'numprocs=2',
        'process_name=%(program_name)s_%(process_num)02d',
        'startsecs=1',
        'startretries=0',
        'stopasgroup=true',
        'killasgroup=true',
        '',
      ].join('\n'),
    )
    execFileSync('supervisord', ['-c', conf])
    const visor = await startVisor('tray-copies', 8779, conf, true)
    try {
      const errors = collectErrors(page)
      await page.goto(visor.url + '/#/tray')
      const attention = page.getByRole('region', { name: 'Needs attention' })
      const lone = trayRow(page, 'e2e-copies:e2e-copies_01', 'Needs attention')
      await expect(lone.getByTestId('tray-time')).toHaveText(/^Fatal( · failing \d+[smhd])?$/, { timeout: 20_000 })
      await expect(lone.getByRole('button')).toHaveText(['Start'])
      await expect(attention.getByTestId('tray-row')).toHaveCount(1)

      // The copies stay together in their config: red, counted, with Stop and Restart for every copy.
      const row = trayRow(page, 'e2e-copies', 'copies.ini')
      await expect(row).toContainText('×2')
      const line = row.getByTestId('tray-time')
      await expect(line).toHaveText('1 of 2 running · 1 failing')
      await expect(line).toHaveClass(/t-bad/)
      await expect(row.getByRole('button', { name: /^(Stop|Restart) e2e-copies$/ })).toHaveText(['Stop', 'Restart'])

      await row.getByRole('button', { name: 'Show the 2 copies of e2e-copies' }).click()
      const copies = page.getByRole('group', { name: 'Copies of e2e-copies' })
      const bad = copies.getByTestId('tray-row').filter({ has: page.getByText('e2e-copies_01', { exact: true }) })
      await expect(bad.getByTestId('tray-time')).toHaveText(/^Fatal/)
      await expect(bad.getByRole('button', { name: 'Start e2e-copies_01', exact: true })).toBeVisible()
      const ok = copies.getByTestId('tray-row').filter({ has: page.getByText('e2e-copies_00', { exact: true }) })
      await expect(ok.getByTestId('tray-time')).toHaveText(/^Running · up \d+[smhd]/)
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
      await stopSupervisord(conf)
    }
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
      await expect(foot.getByRole('button')).toHaveText(['Restart all', 'Apply changes', 'Open Visor…'])
      for (const name of ['Restart all', 'Apply changes']) {
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
