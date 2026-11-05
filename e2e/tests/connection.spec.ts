import fs from 'node:fs'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import { collectErrors, configRow, downConfig, startVisor } from './helpers'

// The server build keeps passwords in an in-memory keychain, so these never
// touch the login Keychain.
test.describe('connection credentials', () => {
  test('a saved password shows as saved, can be changed, and Remove clears the username too', async ({ page }) => {
    await page.goto('/#/settings')
    await page.getByLabel('Username').fill('me')
    await page.getByLabel('Password', { exact: true }).fill('s3cret')
    await page.getByRole('button', { name: 'Save and reconnect' }).click()

    // Saved: a status line, not an empty box.
    await expect(page.getByTestId('password-saved')).toHaveText('Saved in your Keychain')
    await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0)

    // Change… brings the box back; Cancel puts the status line back.
    await page.getByRole('button', { name: 'Change…' }).click()
    await expect(page.getByLabel('Password', { exact: true })).toBeFocused()
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByTestId('password-saved')).toBeVisible()

    await page.getByRole('button', { name: 'Remove password' }).click()
    await expect(page.getByTestId('password-saved')).toBeHidden()
    await expect(page.getByLabel('Username')).toHaveValue('')
    await page.reload()
    await expect(page.getByLabel('Username')).toHaveValue('')
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('')
  })
})

// Its own Visor on a config whose supervisord isn't running, so the shared
// sandbox keeps running.
test.describe('supervisord down', () => {
  test('the dashboard says so plainly, keeps the configs, and disables what needs supervisord', async ({ page }) => {
    test.setTimeout(60_000)
    const conf = downConfig('connection-down-conf')
    const dir = path.dirname(conf)
    fs.writeFileSync(path.join(dir, 'conf.d', 'down.ini'), '[program:down-sleeper]\ncommand=/bin/sleep 600\nautostart=false\n')
    const visor = await startVisor('connection-down', 8777, conf, true)
    try {
      const errors = collectErrors(page)
      // After setup, a failed first connection opens Doctor; the dashboard is one click away.
      await page.goto(visor.url + '/#/')
      await expect(page).toHaveURL(/#\/doctor$/, { timeout: 15_000 })
      await page.getByRole('link', { name: 'Dashboard' }).first().click()
      await expect(page).toHaveURL(/#\/$/)

      // The sidebar's server line says it isn't running.
      await expect(page.getByRole('navigation', { name: 'Servers' })).toContainText('Not running')

      // The banner: a plain title and line, not the Go error.
      const banner = page.getByRole('alert', { name: 'supervisord isn’t running' })
      await expect(banner).toBeVisible()
      await expect(banner).toContainText(`Nothing answers at ${dir}/s.sock, so Visor can’t show or control processes. Your config files are fine.`)
      const raw = banner.getByTestId('connection-error')
      await expect(raw).toBeHidden()
      await expect(banner.getByText(/dial unix/)).toBeHidden()
      // Show details reveals the raw error.
      await banner.getByText('Show details').click()
      await expect(raw).toBeVisible()
      await expect(raw).toContainText('supervisord: unreachable')
      await expect(raw).toContainText(`dial unix ${dir}/s.sock`)

      // The strip: dashes and "Not connected", not zeros; the config count is from disk.
      const strip = page.getByRole('region', { name: 'Health' })
      await expect(strip).toContainText('Not connected')
      await expect(strip.locator('dd')).toHaveText(['—', '—', '—', '1'])
      await expect(strip).not.toContainText('processes are')

      // Bulk actions and Check for changes are disabled, with the reason as their tooltip.
      for (const name of ['Start all', 'Stop all', 'Restart all']) {
        const b = page.getByRole('group', { name: 'All processes' }).getByRole('button', { name, exact: true })
        await expect(b).toBeDisabled()
        await expect(b).toHaveAttribute('title', 'supervisord isn’t running')
      }
      await expect(page.getByRole('button', { name: 'Check for changes' })).toBeDisabled()
      await expect(page.getByRole('link', { name: 'Add new' }).last()).toBeVisible()

      // The config list stays, with the quiet line; its ⋯ menu keeps only the file actions.
      await expect(configRow(page, 'down.ini')).toBeVisible()
      await expect(page.getByText('Processes appear once supervisord is back')).toBeVisible()
      await configRow(page, 'down.ini').getByRole('button', { name: 'Actions for down.ini' }).click()
      for (const item of ['Start all in file', 'Stop all in file', 'Restart all in file', 'Apply changes', 'Remove from supervisor']) {
        const mi = page.getByRole('menuitem', { name: new RegExp(`^${item}`) })
        await expect(mi).toBeDisabled()
        await expect(mi).toHaveAttribute('title', 'supervisord isn’t running')
      }
      await expect(page.getByRole('menuitem', { name: /^Open \/ edit config/ })).toBeEnabled()
      await page.keyboard.press('Escape')
      // The server menu too: no shutting down what isn't running.
      await page.getByRole('button', { name: 'Server actions' }).click()
      await expect(page.getByRole('menuitem', { name: /^Shut down supervisord/ })).toBeDisabled()
      await expect(page.getByRole('menuitem', { name: /^Server settings/ })).toBeEnabled()
      await page.keyboard.press('Escape')

      // Try again reconnects and says when nothing changed.
      await banner.getByRole('button', { name: 'Try again' }).click()
      await expect(page.getByRole('status').filter({ hasText: 'Still not connected: supervisord isn’t running.' })).toBeVisible({ timeout: 15_000 })
      await expect(banner.getByRole('button', { name: 'Try again' })).toBeEnabled()

      // Open Doctor goes to Doctor, which says the same.
      await banner.getByRole('link', { name: 'Open Doctor' }).click()
      await expect(page).toHaveURL(/#\/doctor$/)
      await expect(page.getByRole('heading', { name: 'supervisord isn’t running' })).toBeVisible({ timeout: 15_000 })
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
    }
  })
})
