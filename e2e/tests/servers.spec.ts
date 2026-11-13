import { expect, test, type Page } from '@playwright/test'
import { collectErrors, openApp } from './helpers'

// A server tab against the e2e SSH server (bin/visor-e2e-ssh on
// 127.0.0.1:8767, started by serve.sh) in front of the sandbox supervisord.
// The steps share the one Visor, so they run in order.
test.describe.configure({ mode: 'serial' })

const NAME = 'e2e box'

function tabs(page: Page) {
  return page.getByRole('tablist', { name: 'Servers' })
}

async function fillForm(page: Page) {
  const form = page.getByRole('dialog', { name: 'Add a server' })
  await form.getByLabel('Name', { exact: true }).fill(NAME)
  await form.getByLabel('Host', { exact: true }).fill('127.0.0.1')
  await form.getByLabel('Port', { exact: true }).fill('8767')
  await form.getByLabel('User', { exact: true }).fill('e2e')
  await form.getByLabel('Password', { exact: true }).fill('e2e')
  await form.getByText('Advanced: config path, jump host').click()
  await form.getByLabel('Config path').fill('/tmp/visor-e2e/supervisord.conf')
  return form
}

test.describe('server tabs', () => {
  test('launch shows only this computer’s tab and +', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/')
    await expect(tabs(page).getByRole('tab')).toHaveCount(1)
    await expect(tabs(page).getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: 'Add or open a server' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('the form checks the fields', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Add or open a server' }).click()
    await page.getByRole('menuitem', { name: 'Add a server…' }).click()
    const form = page.getByRole('dialog', { name: 'Add a server' })
    await form.getByRole('button', { name: 'Save' }).click()
    await expect(form.getByRole('alert')).toContainText('Give the server a name')
    await form.getByRole('button', { name: 'Cancel' }).click()
    await expect(form).toBeHidden()
  })

  test('Test asks to trust the host key, then passes; Connect opens a tab', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Add or open a server' }).click()
    await page.getByRole('menuitem', { name: 'Add a server…' }).click()
    const form = await fillForm(page)
    await form.getByRole('button', { name: 'Test' }).click()

    const trust = page.getByRole('dialog', { name: /Trust 127\.0\.0\.1:8767\?/ })
    await expect(trust).toBeVisible()
    await expect(trust.getByTestId('fingerprint')).toContainText('SHA256:')
    await expect(trust).toContainText('~/.ssh/known_hosts isn’t changed')
    await trust.getByRole('button', { name: 'Trust and connect' }).click()

    await expect(form.getByRole('heading', { name: 'Test passed' })).toBeVisible({ timeout: 20_000 })
    for (const row of ['ssh', 'config', 'socket', 'version']) {
      await expect(form.locator(`[data-row="${row}"]`)).toHaveAttribute('data-status', 'ok')
    }
    await form.getByRole('button', { name: 'Connect' }).click()

    const tab = tabs(page).getByRole('tab', { name: new RegExp(NAME) })
    await expect(tab).toHaveAttribute('aria-selected', 'true', { timeout: 20_000 })
    await expect(tab).toContainText('DEV')
    await expect(page).toHaveURL(/#\/s\/s[0-9a-f]{10}/)
    await expect(page.getByRole('link', { name: 'reports.ini', exact: true })).toBeVisible({ timeout: 20_000 })
    // A server tab: its report, no Add new.
    const nav = page.getByRole('navigation', { name: NAME })
    await expect(nav.getByRole('link', { name: 'Server report' })).toBeVisible()
    await expect(nav.getByRole('link', { name: /Add new/ })).toHaveCount(0)
    expect(errors).toEqual([])
  })

  test('links stay in the tab; switching tabs keeps each tab’s page', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Add or open a server' }).click()
    const saved = page.getByRole('menuitem', { name: new RegExp(NAME) })
    // A reload keeps the open tab (it is still open in Go).
    if (await saved.count()) await saved.click()
    else await page.keyboard.press('Escape')
    await tabs(page).getByRole('tab', { name: new RegExp(NAME) }).click()

    await page.getByRole('navigation', { name: NAME }).getByRole('link', { name: 'Issues' }).click()
    await expect(page).toHaveURL(/#\/s\/s[0-9a-f]{10}\/issues/)

    await tabs(page).getByRole('tab', { name: /This computer/ }).click()
    await expect(page).toHaveURL(/#\/$/)
    await expect(page.getByRole('navigation', { name: 'Servers' }).getByRole('link', { name: /Add new/ })).toBeVisible()

    await tabs(page).getByRole('tab', { name: new RegExp(NAME) }).click()
    await expect(page).toHaveURL(/\/issues/)

    // Ctrl+Tab moves on, wrapping round to this computer.
    await page.keyboard.press('Control+Tab')
    await expect(tabs(page).getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')
  })

  test('closing the tab disconnects; + opens the saved server again', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Add or open a server' }).click()
    const saved = page.getByRole('menuitem', { name: new RegExp(NAME) })
    if (await saved.count()) await saved.click()
    else await page.keyboard.press('Escape')
    const tab = tabs(page).getByRole('tab', { name: new RegExp(NAME) })
    await expect(tab).toBeVisible()
    await tab.getByRole('button', { name: `Close ${NAME} and disconnect` }).click()
    await expect(tab).toHaveCount(0)
    await expect(tabs(page).getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')

    await page.getByRole('button', { name: 'Add or open a server' }).click()
    await page.getByRole('menuitem', { name: new RegExp(NAME) }).click()
    await expect(tab).toHaveAttribute('aria-selected', 'true', { timeout: 20_000 })
  })

  test('Settings lists the server; Delete removes it and closes its tab', async ({ page }) => {
    await openApp(page, '/settings')
    const card = page.getByRole('region', { name: 'Servers' })
    const row = card.locator(`[data-server="${NAME}"]`)
    await expect(row).toContainText('e2e@127.0.0.1:8767')
    await row.getByRole('button', { name: `Delete ${NAME}` }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toHaveCount(0)
    await expect(tabs(page).getByRole('tab')).toHaveCount(1)
  })
})
