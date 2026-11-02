import { expect, test } from '@playwright/test'
import { collectErrors, waitForDashboard } from './helpers'

test.describe('notifications', () => {
  test('the bell counts unread failures and leads to them', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    // reports-sync can't start in the sandbox, so a notification is waiting.
    const bell = page.getByTestId('notification-bell')
    await expect(bell).toHaveAccessibleName(/Notifications, \d+ unread/, { timeout: 15_000 })
    await bell.click()

    const panel = page.getByRole('dialog', { name: 'Notifications' })
    await expect(panel).toContainText('This build can’t show macOS banners')
    await panel.getByRole('button', { name: 'All', exact: true }).click()
    const item = panel.getByRole('button', { name: /reports-sync can.t start/ })
    await expect(item).toContainText('reports.ini')
    await item.click()
    await expect(panel).toBeHidden()
    await expect(page.getByRole('heading', { level: 1, name: 'reports.ini' })).toBeVisible()

    await bell.click()
    await panel.getByRole('button', { name: 'All', exact: true }).click()
    await expect(panel.getByRole('button', { name: /reports-sync can.t start/ })).toBeVisible()
    await expect(panel.getByRole('button', { name: /reports-sync can.t start, unread/ })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(panel).toBeHidden()
    await expect(bell).toBeFocused()

    await bell.click()
    await panel.getByRole('button', { name: 'Mark all as read' }).click()
    await expect(bell).toHaveAccessibleName('Notifications')
    await expect(panel.getByRole('button', { name: 'Mark all as read' })).toBeDisabled()
    expect(errors).toEqual([])
  })
})
