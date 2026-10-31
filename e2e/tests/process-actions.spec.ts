import { expect, test } from '@playwright/test'
import { expectState, processAction, waitForDashboard } from './helpers'

test.describe('process actions', () => {
  test('starts and stops a process from its menu', async ({ page }) => {
    await waitForDashboard(page)
    await expectState(page, 'reports-export', 'STOPPED')

    await processAction(page, 'reports-export', 'Start')
    await expectState(page, 'reports-export', 'RUNNING')

    await processAction(page, 'reports-export', 'Stop')
    await expectState(page, 'reports-export', 'STOPPED')
  })

  test('Stop all asks first, and Cancel stops nothing', async ({ page }) => {
    await waitForDashboard(page)
    await expectState(page, 'shop-scheduler', 'RUNNING')

    await page.getByRole('button', { name: 'Stop all', exact: true }).first().click()
    const dialog = page.getByRole('alertdialog', { name: /Stop all processes/ })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toBeHidden()

    await expectState(page, 'shop-scheduler', 'RUNNING')
    await expectState(page, 'mailer-worker:mailer-worker_00', 'RUNNING')
  })
})
