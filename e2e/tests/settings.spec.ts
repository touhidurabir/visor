import { expect, test } from '@playwright/test'

test.describe('settings', () => {
  test('the config path is fixed by VISOR_CONFIG', async ({ page }) => {
    await page.goto('/#/settings')
    await expect(page.getByText('Set by VISOR_CONFIG').first()).toBeVisible()
    await expect(page.getByLabel('Config file')).toHaveValue('/tmp/visor-e2e/supervisord.conf')
    await expect(page.getByRole('button', { name: 'Choose…' })).toBeDisabled()
  })

  test('the cards fill a wide window in two columns and stack in a narrow one', async ({ page }) => {
    await page.setViewportSize({ width: 1600, height: 900 })
    await page.goto('/#/settings')
    const card = (name: string) => page.getByRole('group', { name })
    await expect(card('Connection')).toBeVisible()
    const conn = (await card('Connection').boundingBox())!
    expect(conn.width).toBeGreaterThan(1000)
    const poll = (await card('Polling').boundingBox())!
    const crash = (await card('Crash loops').boundingBox())!
    expect(poll.x).toBeLessThan(crash.x)
    expect(crash.y).toBe(poll.y)

    await page.setViewportSize({ width: 900, height: 900 })
    await expect(async () => {
      const p = (await card('Polling').boundingBox())!
      const n = (await card('Crash loops').boundingBox())!
      expect(n.y).toBeGreaterThan(p.y)
      expect(n.x).toBe(p.x)
    }).toPass()
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
