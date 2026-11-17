import { expect, test, type Page } from '@playwright/test'
import { collectErrors, openApp, processAction, words } from './helpers'

// A server tab against the e2e SSH server (bin/visor-e2e-ssh on
// 127.0.0.1:8767, started by serve.sh) in front of the sandbox supervisord.
// The steps share the one Visor, so they run in order.
test.describe.configure({ mode: 'serial' })

const NAME = 'e2e box'

function tabs(page: Page) {
  return page.getByRole('tablist', { name: 'Servers' })
}

async function openAdd(page: Page) {
  if (!(await page.getByRole('heading', { level: 1, name: 'Remote servers' }).count())) {
    await page.getByRole('navigation', { name: 'Servers' }).getByRole('link', { name: 'Remote servers' }).click()
  }
  await page.getByRole('button', { name: 'Add a server…' }).click()
}

async function fillForm(page: Page) {
  const form = page.getByRole('dialog', { name: 'Add a server' })
  await form.getByLabel('Name', { exact: true }).fill(NAME)
  await form.getByLabel('Host', { exact: true }).fill('127.0.0.1')
  await form.getByLabel('Port', { exact: true }).fill('8767')
  await form.getByLabel('User', { exact: true }).fill('e2e')
  await form.getByLabel('Password', { exact: true }).fill('e2e')
  await form.getByText('Advanced: config path, jump host, notifications').click()
  await form.getByLabel('Config path').fill('/tmp/visor-e2e/supervisord.conf')
  return form
}

test.describe('server tabs', () => {
  test('launch shows only this computer’s tab and +', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/')
    await expect(tabs(page).getByRole('tab')).toHaveCount(1)
    await expect(tabs(page).getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: 'Open a server' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('+ lists saved servers only; with none, it points to Remote servers', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Open a server' }).click()
    const menu = page.getByRole('menu', { name: 'Servers to open' })
    await expect(menu).toContainText('No saved servers yet.')
    await expect(menu.getByRole('menuitem')).toHaveCount(0)
    await menu.getByRole('link', { name: 'Remote servers' }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'Remote servers' })).toBeVisible()
  })

  test('the form checks the fields', async ({ page }) => {
    await openApp(page, '/servers')
    await openAdd(page)
    const form = page.getByRole('dialog', { name: 'Add a server' })
    await form.getByRole('button', { name: 'Save' }).click()
    await expect(form.getByRole('alert')).toContainText('Give the server a name')
    await form.getByRole('button', { name: 'Cancel' }).click()
    await expect(form).toBeHidden()
  })

  test('Test asks to trust the host key, then passes; Connect opens a tab', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/servers')
    await openAdd(page)
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
    await form.getByRole('button', { name: 'Connect', exact: true }).click()

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

  test('a server that can’t connect opens no tab: the form stays and says why', async ({ page }) => {
    await openApp(page, '/servers')
    await openAdd(page)
    const form = await fillForm(page)
    await form.getByLabel('Name', { exact: true }).fill('wrong pw')
    await form.getByLabel('Password', { exact: true }).fill('nope')
    await form.getByRole('button', { name: 'Connect', exact: true }).click()
    // Saved anyway, so the form now edits it, without Connect without saving.
    const edit = page.getByRole('dialog', { name: 'Edit wrong pw' })
    await expect(edit.getByRole('heading', { name: 'Visor couldn’t connect' })).toBeVisible({ timeout: 20_000 })
    await expect(edit.locator('[data-row="ssh"]')).toHaveAttribute('data-status', 'error')
    await expect(edit.getByRole('button', { name: 'Connect without saving' })).toHaveCount(0)
    await expect(tabs(page).getByRole('tab', { name: /wrong pw/ })).toHaveCount(0)
    await edit.getByRole('button', { name: 'Cancel' }).click()
    const row = page.getByRole('region', { name: 'Saved servers' }).locator('[data-server="wrong pw"]')
    await expect(row.getByTestId('server-status')).toHaveText('Not open')
    await row.getByRole('button', { name: 'Delete wrong pw' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toHaveCount(0)
  })

  test('Connect without saving opens a tab and keeps nothing once it closes', async ({ page }) => {
    await openApp(page, '/servers')
    await openAdd(page)
    const form = await fillForm(page)
    await form.getByLabel('Name', { exact: true }).fill('passing by')
    await form.getByRole('button', { name: 'Connect without saving' }).click()
    const tab = tabs(page).getByRole('tab', { name: /passing by/ })
    await expect(tab).toHaveAttribute('aria-selected', 'true', { timeout: 20_000 })
    await page.getByRole('navigation', { name: 'passing by' }).getByRole('link', { name: 'Remote servers' }).click()
    const row = page.getByRole('region', { name: 'Saved servers' }).locator('[data-server="passing by"]')
    await expect(row).toContainText('Not saved')
    await row.getByRole('button', { name: 'Close passing by' }).click()
    await expect(tab).toHaveCount(0)
    await expect(row).toHaveCount(0)
  })

  test('links stay in the tab; switching tabs keeps each tab’s page', async ({ page }) => {
    await openApp(page, '/')
    await page.getByRole('button', { name: 'Open a server' }).click()
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
    await page.getByRole('button', { name: 'Open a server' }).click()
    const saved = page.getByRole('menuitem', { name: new RegExp(NAME) })
    if (await saved.count()) await saved.click()
    else await page.keyboard.press('Escape')
    const tab = tabs(page).getByRole('tab', { name: new RegExp(NAME) })
    await expect(tab).toBeVisible()
    await tab.getByRole('button', { name: `Close ${NAME} and disconnect` }).click()
    await expect(tab).toHaveCount(0)
    await expect(tabs(page).getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')

    await page.getByRole('button', { name: 'Open a server' }).click()
    await page.getByRole('menuitem', { name: new RegExp(NAME) }).click()
    await expect(tab).toHaveAttribute('aria-selected', 'true', { timeout: 20_000 })
  })

  test('configs on a server are read only: the page says so and offers Download', async ({ page }) => {
    await openApp(page, '/')
    const tab = tabs(page).getByRole('tab', { name: new RegExp(NAME) })
    if (!(await tab.count())) {
      await page.getByRole('button', { name: 'Open a server' }).click()
      await page.getByRole('menuitem', { name: new RegExp(NAME) }).click()
    }
    await tab.click()
    await page.getByRole('link', { name: 'reports.ini', exact: true }).click()
    await expect(page).toHaveURL(/#\/s\/s[0-9a-f]{10}\/config\//)
    await expect(page.getByText('can’t change configs on servers')).toBeVisible()
    await expect(page.getByRole('link', { name: 'How to allow editing' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Download a copy' })).toBeVisible()
    await expect(page.getByRole('button', { name: words.reveal })).toHaveCount(0)

    await tab.click() // back to its dashboard
    await page.getByRole('navigation', { name: NAME }).getByRole('link', { name: 'Dashboard' }).click()
    await page.getByRole('button', { name: 'Actions for reports.ini' }).click()
    await expect(page.getByRole('menuitem', { name: /^Download a copy/ })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: /^Remove from supervisor/ })).toBeDisabled()
    await page.keyboard.press('Escape')
  })

  test('a production server asks before Restart, naming the server', async ({ page }) => {
    await openApp(page, '/servers')
    const card = page.getByRole('region', { name: 'Saved servers' })
    await card.locator(`[data-server="${NAME}"]`).getByRole('button', { name: `Edit ${NAME}` }).click()
    const form = page.getByRole('dialog', { name: `Edit ${NAME}` })
    await form.getByLabel('Tag').selectOption('production')
    await form.getByRole('button', { name: 'Save' }).click()
    await expect(form).toBeHidden()

    const tab = tabs(page).getByRole('tab', { name: new RegExp(NAME) })
    await expect(tab).toContainText('PRODUCTION')
    await tab.click()
    await page.getByRole('navigation', { name: NAME }).getByRole('link', { name: 'Dashboard' }).click()
    await processAction(page, 'reports-sync:reports-sync', 'Restart')
    const ask = page.getByRole('alertdialog', { name: `Restart reports-sync:reports-sync on ${NAME}?` })
    await expect(ask).toBeVisible()
    await expect(ask).toContainText('tagged Production')
    await ask.getByRole('button', { name: 'Cancel' }).click()
    await expect(ask).toBeHidden()
  })

  test('a Read only server shows no actions', async ({ page }) => {
    await openApp(page, '/servers')
    const card = page.getByRole('region', { name: 'Saved servers' })
    await card.locator(`[data-server="${NAME}"]`).getByRole('button', { name: `Edit ${NAME}` }).click()
    const form = page.getByRole('dialog', { name: `Edit ${NAME}` })
    await form.getByRole('switch', { name: 'Read only' }).check()
    await form.getByRole('button', { name: 'Save' }).click()
    await expect(form).toBeHidden()
    await tabs(page).getByRole('tab', { name: new RegExp(NAME) }).click()
    await page.getByRole('navigation', { name: NAME }).getByRole('link', { name: 'Dashboard' }).click()
    await expect(page.getByRole('link', { name: 'reports.ini', exact: true })).toBeVisible()
    await expect(page.getByRole('group', { name: 'All processes' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Actions for reports-sync:reports-sync' }).click()
    await expect(page.getByRole('menuitem', { name: /^Restart/ })).toHaveCount(0)
    await expect(page.getByRole('menuitem', { name: /^View logs/ })).toBeVisible()
    await page.keyboard.press('Escape')
  })

  test('the popover has a section for the open server; every section starts folded', async ({ page }) => {
    await openApp(page, '/tray')
    await expect(page.getByText('This computer and 1 server')).toBeVisible()
    const local = page.getByTestId('tray-local')
    await expect(local).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByTestId('tray-row')).toHaveCount(0)
    await local.click()
    await expect(page.getByTestId('tray-row').first()).toBeVisible()
    const sec = page.getByRole('region', { name: NAME })
    await expect(sec).toBeVisible()
    await expect(sec).toContainText('PRODUCTION')
    await sec.getByRole('button', { name: new RegExp(NAME) }).click()
    await expect(sec.getByTestId('tray-server-row').first()).toBeVisible()
    // Read only: no buttons on its rows.
    await expect(sec.getByRole('button', { name: /^Restart .* on / })).toHaveCount(0)
    await expect(sec.getByRole('button', { name: 'Open tab' })).toBeVisible()
  })

  test('Remote servers lists the server; Delete removes it and closes its tab', async ({ page }) => {
    await openApp(page, '/servers')
    const card = page.getByRole('region', { name: 'Saved servers' })
    const row = card.locator(`[data-server="${NAME}"]`)
    await expect(row).toContainText('e2e@127.0.0.1:8767')
    await expect(row.getByTestId('server-status')).toHaveText('Connected')
    await row.getByRole('button', { name: `Delete ${NAME}` }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(row).toHaveCount(0)
    await expect(tabs(page).getByRole('tab')).toHaveCount(1)
  })
})
