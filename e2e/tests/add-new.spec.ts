import { existsSync, readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, expectState, openApp, removeConfig, supervisorStates, waitForDashboard } from './helpers'

// Creates e2e-task.ini in the sandbox's conf.d and takes it out again, so the
// 12 sample programs stay as the other specs expect them.
const FILE = '/tmp/visor-e2e/conf.d/e2e-task.ini'

test.beforeEach(async ({ page }) => {
  await openApp(page, '/') // the cleanup talks to the app, so it has to be loaded
  await removeConfig(page, FILE) // a failed earlier attempt
})
test.afterEach(async ({ page }) => {
  await removeConfig(page, FILE)
})

/** Fills the form from the "Any script" template with a command that stays up. */
async function fillScript(page: Page) {
  await page.getByRole('button', { name: 'Any script' }).click()
  await expect(page.getByLabel('Program name')).toHaveValue('acme-task')
  await page.getByLabel('Program name').fill('e2e-task')
  await page.getByLabel('Command').fill('/bin/sleep 600')
  await page.getByLabel('Working folder').fill('/tmp')
}

test.describe('add new config', () => {
  test('fills the form, keeps it across Write it, and saves and applies', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/new/form')
    await expect(page.getByRole('heading', { level: 1, name: 'Add a new config' })).toBeVisible()
    await fillScript(page)
    await expect(page.getByRole('group', { name: 'Templates' }).getByRole('button', { name: 'Any script' })).toHaveAttribute('aria-pressed', 'true')
    const preview = page.getByLabel('File contents')
    await expect(preview).toContainText('[program:e2e-task]')
    await expect(page.getByText('/tmp/visor-e2e/conf.d/e2e-task.ini')).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: 'Ready to save' })).toBeVisible()

    // Form -> Write it: the text is there, and a hand edit comes back to the form.
    await page.getByRole('link', { name: 'Write it' }).click()
    const editor = page.getByRole('textbox', { name: 'Config contents' })
    await expect(editor).toContainText('command=/bin/sleep 600')
    await expect(page.getByLabel('File name')).toHaveValue('e2e-task.ini')
    await editor.click()
    await page.keyboard.press('ControlOrMeta+End')
    await page.keyboard.insertText('stopwaitsecs=7\n')
    await page.getByRole('link', { name: 'Fill a form' }).click()
    await expect(page.getByLabel('Program name')).toHaveValue('e2e-task')
    await expect(page.getByLabel('Command')).toHaveValue('/bin/sleep 600')
    await expect(page.getByLabel('Seconds to stop gracefully')).toHaveValue('7')

    // An error in a field blocks saving.
    const save = page.getByRole('button', { name: 'Save & apply' })
    await page.getByLabel('Copies').fill('3')
    await expect(page.locator('#m-numprocs')).toContainText('needs %(process_num)')
    await expect(page.locator('#m-process_name')).toContainText('needs %(process_num)')
    await expect(save).toBeDisabled()
    await page.getByLabel('Copies').fill('1')
    await expect(save).toBeEnabled()
    expect(existsSync(FILE)).toBe(false) // nothing is written before Save & apply

    await save.click()
    const result = page.getByRole('region', { name: 'Saved and applied' })
    await expect(result).toContainText('supervisord added e2e-task')
    expect(readFileSync(FILE, 'utf8')).toContain('stopwaitsecs=7')
    await expect.poll(async () => (await supervisorStates())['e2e-task:e2e-task'], { timeout: 15_000 }).toBe('RUNNING')

    // It is on the dashboard, watched and running.
    await result.getByRole('link', { name: 'Open it' }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'e2e-task.ini' })).toBeVisible()
    await expect(page.getByRole('button', { name: /Watching/ })).toHaveAttribute('aria-pressed', 'true')
    await openApp(page, '/')
    await waitForDashboard(page)
    await expectState(page, 'e2e-task:e2e-task', 'RUNNING')
    const row = page.locator('.cfg-row').filter({ has: page.getByRole('link', { name: 'e2e-task.ini' }) })
    await expect(row.getByRole('button', { name: 'Watching' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('writes it by hand, with checks as you type, and never overwrites a file', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/new')
    const editor = page.getByRole('textbox', { name: 'Config contents' })
    await expect(editor).toContainText('[program:acme-queue]')
    const save = page.getByTestId('save-new')
    const lines = page.getByRole('region', { name: 'Line by line' })

    // A file name outside the include pattern is an error.
    await page.getByLabel('File name').fill('e2e-task.conf')
    await expect(lines).toContainText('does not match the include setting')
    await expect(save).toBeDisabled()
    await expect(save).toHaveText(/^Fix \d+ problems? to save$/)

    // A name that is taken: the checks do not mind, the save refuses clearly.
    await editor.click()
    await page.keyboard.press('ControlOrMeta+a')
    await page.keyboard.insertText('[program:e2e-task]\ncommand=/bin/sleep 600\ndirectory=/tmp\n')
    await page.getByLabel('File name').fill('notes.ini')
    await expect(save).toHaveText('Save & apply')
    await expect(lines).not.toContainText('Error.')
    await expect(page.getByTestId('explain-summary')).toContainText('e2e-task runs the program at /bin/sleep')
    await expect(lines).toContainText('Runs it inside /tmp.')
    const notes = readFileSync('/tmp/visor-e2e/conf.d/notes.ini', 'utf8')
    await save.click()
    await expect(page.getByRole('alert').filter({ hasText: 'already exists' })).toBeVisible()
    expect(readFileSync('/tmp/visor-e2e/conf.d/notes.ini', 'utf8')).toBe(notes)

    // The default name follows the program once the field is emptied.
    await page.getByLabel('File name').fill('')
    await expect(page.getByLabel('File name')).toHaveValue('e2e-task.ini')
    await save.click()
    await expect(page.getByRole('region', { name: 'Saved and applied' })).toBeVisible()
    expect(existsSync(FILE)).toBe(true)
    // The refused save is an expected 422 from the app, which the browser logs.
    expect(errors.filter((e) => !e.includes('422'))).toEqual([])
  })

  test('opens with ⌘N', async ({ page }) => {
    await waitForDashboard(page)
    await page.keyboard.press('Meta+n')
    await expect(page).toHaveURL(/#\/new$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Add a new config' })).toBeVisible()
  })
})
