import { expect, test } from '@playwright/test'
import { waitForDashboard } from './helpers'

test.describe('config detail', () => {
  test('overview flags a missing command', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('link', { name: 'reports.ini', exact: true }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'reports.ini' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: /reports-sync/ })).toBeVisible()

    const row = page.locator('.row').filter({ hasText: '/nonexistent/sync' })
    await expect(row).toContainText('missing')
  })

  test('logs tab follows the process picked in the chips', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('link', { name: 'shop-api.ini', exact: true }).click()
    await page.getByRole('button', { name: /^shop-scheduler/ }).click()
    await page.getByRole('tab', { name: 'Logs' }).click()
    const log = page.getByLabel('stdout log of shop-scheduler')
    await expect(log).toContainText('tick', { timeout: 15_000 })
    await expect(page.getByText('Following live.')).toBeVisible()
  })

  test('history tab renders', async ({ page }) => {
    await page.goto('/#/config/' + encodeURIComponent('/tmp/visor-e2e/conf.d/shop-api.ini') + '?tab=history&process=shop-scheduler')
    await expect(page.getByRole('tab', { name: 'History' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tabpanel')).toContainText(/RUNNING/, { timeout: 15_000 })
  })
})
