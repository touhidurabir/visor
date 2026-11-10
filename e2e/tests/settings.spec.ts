import { expect, test } from '@playwright/test'
import { words } from './helpers'

test.describe('settings', () => {
  test('the config path is fixed by VISOR_CONFIG', async ({ page }) => {
    await page.goto('/#/settings')
    await expect(page.getByText('Set by VISOR_CONFIG').first()).toBeVisible()
    await expect(page.getByTestId('config-path')).toHaveText('/tmp/visor-e2e/supervisord.conf')
    await expect(page.getByRole('button', { name: 'Choose…' })).toBeDisabled()
  })

  test('General comes first, then Connection, Notifications, and Checking and history', async ({ page }) => {
    await page.goto('/#/settings')
    const cards = page.getByRole('main').getByRole('group')
    await expect(cards.first()).toBeVisible()
    const names = await cards.evaluateAll((els) =>
      els.filter((e) => e.getAttribute('aria-labelledby')).map((e) => document.getElementById(e.getAttribute('aria-labelledby')!)?.textContent?.trim()),
    )
    expect(names).toEqual(['General', 'Connection', 'Notifications', 'Checking and history'])
    // The old separate cards are gone.
    for (const old of ['Polling', 'Crash loops', 'Issue history']) await expect(page.getByRole('group', { name: old, exact: true })).toHaveCount(0)

    // General holds Launch at login, Appearance and the tray count.
    const general = page.getByRole('group', { name: 'General' })
    await expect(general.getByRole('switch', { name: 'Launch at login' })).toBeVisible()
    await expect(general.getByRole('radiogroup', { name: 'Appearance' })).toBeVisible()
    await expect(general.getByRole('switch', { name: words.countSwitch })).toBeVisible()

    // One card checks and keeps history: the poll interval, crash loops and retention.
    const checking = page.getByRole('group', { name: 'Checking and history' })
    await expect(checking.getByLabel('Check supervisord every')).toBeVisible()
    await expect(checking.getByLabel('Restarts')).toBeVisible()
    await expect(checking.getByLabel('Minutes')).toBeVisible()
    await expect(checking.getByLabel('Keep resolved issues and process history for')).toBeVisible()
    // Number fields are narrow and right-aligned, with the unit after.
    const poll = checking.getByLabel('Check supervisord every')
    await expect(poll).toHaveCSS('text-align', 'right')
    expect((await poll.boundingBox())!.width).toBeLessThanOrEqual(72)

    // The saved password row stays in Connection (connection.spec covers it).
    await expect(page.getByRole('group', { name: 'Connection' }).getByText(words.keptPassword)).toBeVisible()
  })

  test('each row has the label on the left and the control on the right', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/#/settings')
    const label = (await page.getByText('Config file', { exact: true }).boundingBox())!
    const path = (await page.getByTestId('config-path').boundingBox())!
    expect(path.x).toBeGreaterThan(label.x + label.width)
    expect(Math.abs(path.y - label.y)).toBeLessThan(12)
    // "All changes saved" sits next to the title.
    const title = (await page.getByRole('heading', { name: 'Settings', level: 1 }).boundingBox())!
    const saved = (await page.getByText('All changes saved').boundingBox())!
    expect(saved.x).toBeLessThan(title.x + title.width + 40)
    expect(Math.abs(saved.y + saved.height / 2 - (title.y + title.height / 2))).toBeLessThan(8)
  })

  test('the notification kinds are off while Send notifications is off', async ({ page }) => {
    await page.goto('/#/settings')
    const card = page.getByRole('group', { name: 'Notifications' })
    const main = card.getByRole('switch', { name: 'Send notifications' })
    const kinds = card.getByRole('group', { name: 'Notify me when' }).getByRole('checkbox')
    await expect(kinds).toHaveCount(4)
    await expect(main).toBeChecked()
    for (const k of await kinds.all()) await expect(k).toBeEnabled()
    await main.setChecked(false)
    await expect(page.getByText('All changes saved')).toBeVisible()
    for (const k of await kinds.all()) await expect(k).toBeDisabled()
    // Put it back so the other tests see the defaults.
    await main.setChecked(true)
    await expect(page.getByText('All changes saved')).toBeVisible()
    for (const k of await kinds.all()) await expect(k).toBeEnabled()
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

  test('notifications say the server build cannot show them', async ({ page }) => {
    await page.goto('/#/settings')
    const card = page.getByRole('group', { name: 'Notifications' })
    await expect(card.getByTestId('notify-permission')).toHaveText('Not available in this build')
    await card.getByRole('button', { name: 'Send a test notification' }).click()
    await expect(page.getByText(/aren't available in this build/)).toBeVisible()
    await expect(card.getByRole('button', { name: 'Send a test notification' })).toBeEnabled()
  })
})
