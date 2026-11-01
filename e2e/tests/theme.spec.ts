import { expect, test, type Page } from '@playwright/test'

const theme = (page: Page) => page.evaluate(() => document.documentElement.dataset.theme)
const sidebarSwitch = (page: Page) => page.getByRole('navigation', { name: 'Servers' }).getByRole('radiogroup', { name: 'Appearance' })
const settingsSwitch = (page: Page) => page.getByRole('main').getByRole('radiogroup', { name: 'Appearance' })

test.describe('appearance', () => {
  // The sandbox's settings.json is shared by every spec: leave it on System.
  test.afterEach(async ({ page }) => {
    await page.goto('/#/')
    await sidebarSwitch(page).getByRole('radio', { name: 'System' }).click()
    await expect(sidebarSwitch(page).getByRole('radio', { name: 'System' })).toBeChecked()
  })

  test('System follows the OS, live', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/#/')
    await expect(sidebarSwitch(page).getByRole('radio', { name: 'System' })).toBeChecked()
    expect(await theme(page)).toBe('light')
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect.poll(() => theme(page)).toBe('dark')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect.poll(() => theme(page)).toBe('light')
  })

  test('Light and Dark override the OS and persist across a reload', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/#/')
    await sidebarSwitch(page).getByRole('radio', { name: 'Dark' }).click()
    await expect.poll(() => theme(page)).toBe('dark')
    await expect(page.locator('body')).toHaveCSS('color-scheme', 'dark')

    await page.reload()
    // Applied before the app mounts: no flash of the OS theme.
    expect(await theme(page)).toBe('dark')
    await expect(sidebarSwitch(page).getByRole('radio', { name: 'Dark' })).toBeChecked()

    await page.emulateMedia({ colorScheme: 'dark' })
    await sidebarSwitch(page).getByRole('radio', { name: 'Light' }).click()
    await expect.poll(() => theme(page)).toBe('light')
    await page.reload()
    expect(await theme(page)).toBe('light')
  })

  test('the sidebar and Settings controls stay in sync, and arrows move the choice', async ({ page }) => {
    await page.goto('/#/settings')
    await expect(settingsSwitch(page)).toBeVisible()
    await settingsSwitch(page).getByRole('radio', { name: 'Light' }).click()
    await expect(sidebarSwitch(page).getByRole('radio', { name: 'Light' })).toBeChecked()

    await sidebarSwitch(page).getByRole('radio', { name: 'Light' }).focus()
    await page.keyboard.press('ArrowRight')
    await expect(sidebarSwitch(page).getByRole('radio', { name: 'Dark' })).toBeChecked()
    await expect(settingsSwitch(page).getByRole('radio', { name: 'Dark' })).toBeChecked()
    await expect.poll(() => theme(page)).toBe('dark')
  })

  test('the menu-bar popover follows the saved choice', async ({ page, context }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/#/')
    await sidebarSwitch(page).getByRole('radio', { name: 'Dark' }).click()
    await expect.poll(() => theme(page)).toBe('dark')

    // The popover is another window: it learns the choice from the page cache / settings.
    const tray = await context.newPage()
    await tray.emulateMedia({ colorScheme: 'light' })
    await tray.goto('/#/tray')
    await expect.poll(() => theme(tray)).toBe('dark')
    // And live: switching in the main window reaches the open popover.
    await sidebarSwitch(page).getByRole('radio', { name: 'Light' }).click()
    await expect.poll(() => theme(tray)).toBe('light')
  })
})
