import { expect, test } from '@playwright/test'
import { collectErrors, waitForDashboard, words } from './helpers'

test.describe('notifications', () => {
  test('the bell counts unread failures and leads to them', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    // reports-sync can't start in the sandbox, so a notification is waiting.
    const bell = page.getByTestId('notification-bell')
    await expect(bell).toHaveAccessibleName(/Notifications, \d+ unread/, { timeout: 15_000 })
    await bell.click()

    const panel = page.getByRole('dialog', { name: 'Notifications' })
    // The "no banners" note is one line in the footer, not a box above the list.
    await expect(panel.locator('footer')).toContainText(words.noBanners)

    // Unread titles are bold.
    const anyUnread = panel.getByRole('button', { name: /, unread/ }).first()
    await expect(anyUnread.locator('.title')).toHaveCSS('font-weight', '600')

    await panel.getByRole('button', { name: 'All', exact: true }).click()
    // Grouped by day: the sandbox's failures all happened today.
    const today = panel.getByRole('region', { name: 'Today' })
    const item = today.getByRole('button', { name: /reports-sync can.t start/ })
    await expect(item).toContainText('reports.ini')
    // The severity glyph sits in the title line, with the word for screen
    // readers; there is no separate severity line.
    const title = item.locator('.title')
    await expect(title.locator('svg.sev')).toHaveAttribute('data-severity', 'error')
    await expect(item.locator('svg.sev')).toHaveCount(1)
    await expect(title).toContainText(/Error: reports-sync can.t start/)
    await expect(item.getByText(/^(Error|Warning|Info)$/)).toHaveCount(0)
    const glyph = (await title.locator('svg.sev').boundingBox())!
    const line = (await title.boundingBox())!
    expect(glyph.y).toBeGreaterThanOrEqual(line.y)
    expect(glyph.y + glyph.height).toBeLessThanOrEqual(line.y + 20)
    // And the kind icon leads the item.
    await expect(item.locator('.kind svg')).toHaveCount(1)
    await item.click()
    await expect(panel).toBeHidden()
    await expect(page.getByRole('heading', { level: 1, name: 'reports.ini' })).toBeVisible()

    await bell.click()
    await panel.getByRole('button', { name: 'All', exact: true }).click()
    await expect(panel.getByRole('button', { name: /reports-sync can.t start/ })).toBeVisible()
    await expect(panel.getByRole('button', { name: /reports-sync can.t start, unread/ })).toHaveCount(0)
    // Read titles are regular weight.
    await expect(panel.getByRole('button', { name: /reports-sync can.t start/ }).first().locator('.title')).toHaveCSS('font-weight', '400')
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
