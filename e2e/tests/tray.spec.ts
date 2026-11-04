import { expect, test } from '@playwright/test'
import { collectErrors, waitForDashboard } from './helpers'

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
