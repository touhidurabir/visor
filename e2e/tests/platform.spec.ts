import { expect, test, type Page } from '@playwright/test'
import { SANDBOX, WORDS, startVisor, type Platform } from './helpers'

// The words that depend on the OS, on both platforms: the shared Visor says
// them as on Linux; a second Visor on the same sandbox supervisord, started
// with VISOR_PLATFORM=darwin, says them as on macOS.

async function checkWords(page: Page, platform: Platform, base: string) {
  const w = WORDS[platform]
  await page.goto(`${base}/#/`)
  await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible({ timeout: 15_000 })

  // Sidebar shortcut hints and the dashboard's search hint.
  const addNew = page.getByRole('navigation', { name: 'Servers' }).getByRole('link', { name: /Add new/ })
  await expect(addNew).toContainText(w.newConfigHint)
  await expect(addNew).toHaveAttribute('aria-keyshortcuts', `${w.mod}+N`)
  await expect(page.locator('.kbd').filter({ hasText: w.findHint })).toBeVisible()

  // The shortcut uses the platform's modifier, and only that one.
  await page.locator('body').click({ position: { x: 5, y: 5 } })
  const other = platform === 'darwin' ? 'Control' : 'Meta'
  await page.keyboard.press(`${other}+n`)
  await expect(page).not.toHaveURL(/#\/new$/)
  await page.keyboard.press(`${w.mod}+n`)
  await expect(page).toHaveURL(/#\/new$/)

  // A config's file-manager button.
  await page.goto(`${base}/#/config/` + encodeURIComponent(`${SANDBOX}/conf.d/reports.ini`))
  await expect(page.getByRole('button', { name: w.reveal }).first()).toBeVisible()

  // Settings: the tray count switch, the password store and launch at login.
  await page.goto(`${base}/#/settings`)
  const general = page.getByRole('group', { name: 'General' })
  await expect(general.getByRole('switch', { name: w.countSwitch })).toBeVisible()
  await expect(general.getByText(w.loginUnsupported)).toBeVisible()
  await expect(page.getByRole('group', { name: 'Connection' }).getByText(w.keptPassword)).toBeVisible()
  // The macOS-only notification row (System Settings) is not on Linux.
  const notifications = page.getByRole('group', { name: 'Notifications' })
  await expect(notifications.getByRole('button', { name: 'Open Notification Settings' })).toHaveCount(platform === 'darwin' ? 1 : 0)

  // The popover closes on focus loss on macOS; on Linux it has a close button.
  await page.goto(`${base}/#/tray`)
  await expect(page.getByText(/\d+ failing/)).toBeVisible()
  await expect(page.getByTestId('tray-close')).toHaveCount(platform === 'darwin' ? 0 : 1)
}

test.describe('platform wording', () => {
  test('the shared Visor words things as on Linux', async ({ page, baseURL }) => {
    await checkWords(page, 'linux', baseURL!)
  })

  test('a Visor told it runs on macOS words things as on a Mac', async ({ page }) => {
    const visor = await startVisor('platform-darwin', 8780, `${SANDBOX}/supervisord.conf`, true, 'darwin')
    try {
      await checkWords(page, 'darwin', visor.url)
    } finally {
      await visor.stop()
    }
  })
})
