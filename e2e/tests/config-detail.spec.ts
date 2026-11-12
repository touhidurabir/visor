import { unlinkSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { SANDBOX, openApp, waitForDashboard, words } from './helpers'

const configPage = (file: string, query = '') => `/config/${encodeURIComponent(`${SANDBOX}/conf.d/${file}`)}${query}`

/** The computed colour of a design token, as getComputedStyle reports colours. */
async function token(page: Page, name: string, prop: 'color' | 'backgroundColor' = 'color'): Promise<string> {
  return page.evaluate(
    ([n, p]) => {
      const el = document.createElement('div')
      el.style[p as 'color'] = `var(${n})`
      document.body.append(el)
      const c = getComputedStyle(el)[p as 'color']
      el.remove()
      return c
    },
    [name, prop],
  )
}

test.describe('config detail', () => {
  test('overview flags a missing command', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('link', { name: 'reports.ini', exact: true }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'reports.ini' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('group', { name: 'Processes in this config' }).getByRole('button', { name: /reports-sync/ })).toBeVisible()

    const lines = page.getByRole('region', { name: 'What this config does' })
    const row = lines.getByRole('listitem').filter({ hasText: 'command=/nonexistent/sync' })
    await expect(row).toContainText('missing')
    await expect(row).toContainText('Error.')
    await expect(row).toContainText('How to fix:')
    await expect(row.getByRole('button', { name: words.reveal })).toBeDisabled()

    // The last error card points at that line.
    const card = page.getByRole('region', { name: /Last error/ })
    await expect(card).toContainText('Caused by line')

    // "Edit line N" opens the editor on that line.
    await row.getByRole('button', { name: /^Edit line \d+$/ }).click()
    await expect(page.getByRole('tab', { name: 'Config' })).toHaveAttribute('aria-selected', 'true')
    await expect(page).toHaveURL(/line=\d+/)
  })

  test('a log path opens its process in the Logs tab', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('link', { name: 'shop-api.ini', exact: true }).click()
    const lines = page.getByRole('region', { name: 'What this config does' })
    // Nothing in shop-api.ini is failing, so its cards start closed.
    await lines.getByRole('button', { name: 'Expand all' }).click()
    await lines.getByRole('button', { name: 'View in Logs tab' }).first().click()
    await expect(page.getByRole('tab', { name: 'Logs' })).toHaveAttribute('aria-selected', 'true')
  })

  test('logs tab follows the process picked in the chips', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('link', { name: 'shop-api.ini', exact: true }).click()
    await page.getByRole('button', { name: /^shop-scheduler/ }).click()
    await page.getByRole('tab', { name: 'Logs' }).click()
    const log = page.getByLabel('stdout log of shop-scheduler')
    await expect(log).toContainText('tick', { timeout: 15_000 })

    // "Following live" sits in the log toolbar, so it stays in view while the log scrolls.
    const bar = page.locator('.logs .bar')
    const follow = bar.getByRole('status')
    await expect(follow).toHaveText('Following live')
    await bar.getByRole('button', { name: 'Pause' }).click()
    await expect(follow).toHaveText(/^Paused/)
    await bar.getByRole('button', { name: 'Resume' }).click()
    await expect(follow).toHaveText('Following live')

    // Clear empties the log, so it is red (a running process's logs can be cleared).
    const clear = bar.getByRole('button', { name: 'Clear', exact: true })
    await expect(clear).toBeEnabled()
    await expect(clear).toHaveClass(/\bdanger\b/)
    expect(await clear.evaluate((el) => getComputedStyle(el).color)).toBe(await token(page, '--bad'))
  })

  test('the header puts Watching first, then a quiet group, and the failing chip is tinted and labelled', async ({ page }) => {
    await openApp(page, configPage('mailer.ini'))
    await expect(page.getByRole('heading', { level: 1, name: 'mailer.ini' })).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveText(/Local\s*\/\s*mailer\.ini/)
    await expect(page.getByRole('button', { name: /^Watch(ing)?$/ })).toHaveAttribute('aria-pressed', /true|false/)
    const group = page.getByRole('group', { name: 'All processes in mailer.ini' })
    for (const name of ['Start all', 'Stop all', 'Restart all']) await expect(group.getByRole('button', { name, exact: true })).toHaveClass(/\bquiet\b/)

    const chips = page.getByRole('group', { name: 'Processes in this config' })
    const digest = chips.getByRole('button', { name: /^mailer-digest/ })
    await expect(digest).toContainText(/Fatal · (just now|\d+[smhd])/, { timeout: 20_000 })
    await expect(digest).toHaveClass(/\bfailing\b/)
    expect(await digest.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe(await token(page, '--bad-soft', 'backgroundColor'))
    const worker = chips.getByRole('button', { name: /^mailer-worker_00/ })
    await expect(worker).toContainText(/up /)
    await expect(worker).not.toHaveClass(/\bfailing\b/)
    expect(await worker.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe(await token(page, '--bad-soft', 'backgroundColor'))
  })

  test('a config with problems opens its line by line on Only problems', async ({ page }) => {
    await openApp(page, configPage('mailer.ini'))
    const lines = page.getByRole('region', { name: 'What this config does' })
    await expect(lines.getByRole('button', { name: /^Only problems · \d+$/ })).toHaveAttribute('aria-pressed', 'true')
    await expect(lines.getByRole('button', { name: 'All lines' })).toHaveAttribute('aria-pressed', 'false')
    await expect(lines).toContainText('Suggestion.')
    await lines.getByRole('button', { name: 'Expand all' }).click()
    await expect(lines.getByRole('button', { name: /^Program mailer-worker/ })).toHaveAttribute('aria-expanded', 'true')
    await expect(lines.locator('[data-card="program:mailer-worker"]')).toContainText('not set:') // its suggestion
    await expect(lines).not.toContainText('numprocs=2') // a line with no problem is hidden

    // The choice is still the user's.
    await lines.getByRole('button', { name: 'All lines' }).click()
    await expect(lines.getByRole('button', { name: 'All lines' })).toHaveAttribute('aria-pressed', 'true')
    await expect(lines).toContainText('numprocs=2')
  })

  test('a config without problems opens its line by line on All lines', async ({ page }) => {
    // Written for this test and never applied, so supervisord runs nothing from it.
    const file = `${SANDBOX}/conf.d/e2e-clean.ini`
    writeFileSync(
      file,
      ['[program:e2e-clean]', 'command=/bin/sleep 600', 'directory=/tmp', 'autostart=false', 'autorestart=false', 'redirect_stderr=true', `stdout_logfile=${SANDBOX}/logs/e2e-clean.log`, ''].join('\n'),
    )
    try {
      await openApp(page, configPage('e2e-clean.ini'))
      const lines = page.getByRole('region', { name: 'What this config does' })
      await expect(lines).toContainText('[program:e2e-clean]')
      await expect(lines.getByRole('button', { name: 'All lines' })).toHaveAttribute('aria-pressed', 'true')
      await expect(lines.getByRole('button', { name: 'Only problems', exact: true })).toHaveAttribute('aria-pressed', 'false')
      await expect(lines).toContainText('command=/bin/sleep 600')
    } finally {
      unlinkSync(file)
    }
  })

  test('history tab renders', async ({ page }) => {
    await page.goto('/#/config/' + encodeURIComponent('/tmp/visor-e2e/conf.d/shop-api.ini') + '?tab=history&process=shop-scheduler')
    await expect(page.getByRole('tab', { name: 'History' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tabpanel')).toContainText(/Running/, { timeout: 15_000 })
  })
})
