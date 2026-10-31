import { expect, test } from '@playwright/test'

test.describe('settings', () => {
  test('the config path is fixed by VISOR_CONFIG', async ({ page }) => {
    await page.goto('/#/settings')
    await expect(page.getByText('Set by VISOR_CONFIG').first()).toBeVisible()
    await expect(page.getByLabel('Config file')).toHaveValue('/tmp/visor-e2e/supervisord.conf')
    await expect(page.getByRole('button', { name: 'Choose…' })).toBeDisabled()
  })

  test('the polling interval persists across a reload', async ({ page }) => {
    await page.goto('/#/settings')
    const poll = page.getByLabel('Check supervisord every')
    await expect(page.getByText('All changes saved')).toBeVisible()
    await poll.fill('3')
    await poll.blur()
    await expect(page.getByText('All changes saved')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Check supervisord every')).toHaveValue('3')
  })

  test('a notification checkbox persists across a reload', async ({ page }) => {
    await page.goto('/#/settings')
    const box = page.getByRole('checkbox', { name: /A process exits unexpectedly/ })
    await expect(page.getByText('All changes saved')).toBeVisible()
    const was = await box.isChecked()
    await box.setChecked(!was)
    await expect(page.getByText('All changes saved')).toBeVisible()
    await page.reload()
    const after = page.getByRole('checkbox', { name: /A process exits unexpectedly/ })
    await expect(after).toBeChecked({ checked: !was })
    // Put it back so the other tests see the defaults.
    await after.setChecked(was)
    await expect(page.getByText('All changes saved')).toBeVisible()
  })
})
