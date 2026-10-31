import { expect, test } from '@playwright/test'
import { waitForDashboard } from './helpers'

test.describe('dashboard', () => {
  test('shows the connected server and its config files', async ({ page }) => {
    await waitForDashboard(page)
    await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()
    for (const file of ['shop-api.ini', 'mailer.ini', 'reports.ini', 'notes.ini', 'flaky.ini']) {
      await expect(page.getByRole('link', { name: file, exact: true })).toBeVisible()
    }
  })

  test('stat tiles show non-zero totals', async ({ page }) => {
    await waitForDashboard(page)
    const tile = (label: string) => page.locator('.tile').filter({ hasText: label })
    // 5 files, 12 processes (4 + 1 + 2 + 1 + 2 + 1 + 1).
    await expect(tile('Configs').locator('b')).toHaveText('5')
    await expect(tile('Processes').locator('b')).toHaveText('12')
    await expect(async () => {
      expect(Number(await tile('Running').locator('b').innerText())).toBeGreaterThan(0)
    }).toPass()
  })

  test('search focuses with the shortcut and filters config files', async ({ page }) => {
    await waitForDashboard(page)
    await page.locator('body').click({ position: { x: 5, y: 5 } })
    await page.keyboard.press('Meta+f')
    const search = page.getByRole('searchbox', { name: 'Search processes and config files' })
    await expect(search).toBeFocused()
    await search.fill('mailer')
    await expect(page.getByRole('link', { name: 'mailer.ini', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'shop-api.ini', exact: true })).toBeHidden()
    await search.fill('')
    await expect(page.getByRole('link', { name: 'shop-api.ini', exact: true })).toBeVisible()
  })
})
