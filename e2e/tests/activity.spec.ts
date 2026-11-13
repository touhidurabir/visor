import { expect, test } from '@playwright/test'
import { collectErrors, expectState, openApp, supervisorCall, waitForDashboard } from './helpers'

// Changes made outside Visor (here: XML-RPC straight to the sandbox
// supervisord, as supervisorctl would) show on Activity and the dashboard;
// Visor's own show as "By Visor".
test.describe('activity', () => {
  test('a restart outside Visor shows on Activity and the dashboard', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await expectState(page, 'shop-scheduler:shop-scheduler', 'Running')
    await supervisorCall('supervisor.stopProcess', 'shop-scheduler:shop-scheduler')
    await supervisorCall('supervisor.startProcess', 'shop-scheduler:shop-scheduler')

    const strip = page.getByTestId('activity-strip')
    await expect(strip).toContainText('shop-scheduler:shop-scheduler restarted', { timeout: 20_000 })
    await strip.getByRole('link', { name: 'Activity' }).click()
    await expect(page).toHaveURL(/#\/activity/)
    const row = page.locator('[data-kind="process_restarted"][data-by-visor="false"]').filter({ hasText: 'shop-scheduler:shop-scheduler' }).first()
    await expect(row).toContainText('Outside Visor')

    // Filters: By Visor hides it.
    await page.getByRole('radio', { name: 'By Visor' }).click()
    await expect(row).toBeHidden()
    expect(errors).toEqual([])
  })

  test('Settings has the outside-change notifications, all on', async ({ page }) => {
    await openApp(page, '/settings')
    const card = page.getByRole('region', { name: /Changes made outside Visor/ })
    await expect(card.getByRole('checkbox')).toHaveCount(4)
    for (const c of await card.getByRole('checkbox').all()) await expect(c).toBeChecked()
  })
})
