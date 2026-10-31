import { expect, test } from '@playwright/test'
import { expectState, processAction, supervisorStates, waitForDashboard } from './helpers'

test.describe('process actions', () => {
  test('starts and stops a process from its menu', async ({ page }) => {
    await waitForDashboard(page)
    // Retry-safe: start from a stopped process.
    if ((await supervisorStates())['reports-export:reports-export'] === 'RUNNING') {
      await processAction(page, 'reports-export', 'Stop')
    }
    await expectState(page, 'reports-export', 'STOPPED')

    await processAction(page, 'reports-export', 'Start')
    await expectState(page, 'reports-export', 'RUNNING')

    await processAction(page, 'reports-export', 'Stop')
    await expectState(page, 'reports-export', 'STOPPED')
  })

  test('Stop all asks first, and Cancel stops nothing', async ({ page }) => {
    await waitForDashboard(page)
    await expectState(page, 'shop-scheduler', 'RUNNING')
    const before = await supervisorStates()
    // flaky dies on its own, so it is left out.
    const running = Object.entries(before).filter(([n, s]) => s === 'RUNNING' && !n.startsWith('flaky')).map(([n]) => n)
    expect(running.length).toBeGreaterThan(5)

    await page.getByRole('button', { name: 'Stop all', exact: true }).first().click()
    const dialog = page.getByRole('alertdialog', { name: /Stop all processes/ })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toBeHidden()

    // A round trip to Visor after Cancel: anything Cancel wrongly started
    // would have reached supervisord before this returns.
    await page.getByRole('button', { name: 'Check for changes' }).click()
    await expect(page.getByText(/supervisord already runs every config|differ from what supervisord runs/)).toBeVisible()
    const after = await supervisorStates()
    for (const name of running) expect(after[name], name).toBe('RUNNING')

    await expectState(page, 'shop-scheduler', 'RUNNING')
    await expectState(page, 'mailer-worker:mailer-worker_00', 'RUNNING')
  })
})
