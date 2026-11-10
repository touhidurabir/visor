import { existsSync, readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, configRow, expectState, openApp, processRow, removeConfig, supervisorStates, waitForDashboard, words } from './helpers'

// Creates e2e-task.ini (and mailer-copy.ini, a copy of mailer.ini) in the
// sandbox's conf.d and takes them out again, so the 12 sample programs stay
// as the other specs expect them.
const FILE = '/tmp/visor-e2e/conf.d/e2e-task.ini'
const COPY = '/tmp/visor-e2e/conf.d/mailer-copy.ini'

test.beforeEach(async ({ page }) => {
  await openApp(page, '/') // the cleanup talks to the app, so it has to be loaded
  await removeConfig(page, FILE) // a failed earlier attempt
  await removeConfig(page, COPY)
})
test.afterEach(async ({ page }) => {
  await removeConfig(page, FILE)
  await removeConfig(page, COPY)
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
    await expectState(page, 'e2e-task:e2e-task', 'Running')
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
    const lines = page.getByRole('region', { name: 'What this config does' })

    // A file name outside the include pattern is an error.
    await page.getByLabel('File name').fill('e2e-task.conf')
    await expect(lines).toContainText('does not match the include setting')
    await expect(save).toBeDisabled()
    await expect(page.getByTestId('save-bar').getByRole('status')).toHaveText(/^Fix \d+ problems? to save\.$/)

    // A name that is taken: the checks do not mind, the save refuses clearly.
    await editor.click()
    await page.keyboard.press('ControlOrMeta+a')
    await page.keyboard.insertText('[program:e2e-task]\ncommand=/bin/sleep 600\ndirectory=/tmp\n')
    await page.getByLabel('File name').fill('notes.ini')
    await expect(save).toHaveText('Save & apply')
    await expect(save).toBeEnabled()
    await expect(lines).not.toContainText('Error.')
    // The program's card: its name, and what it does in one sentence.
    await expect(lines.getByRole('button', { name: /^Program e2e-task/ })).toBeVisible()
    await expect(lines).toContainText('Runs the program at /bin/sleep and starts with supervisord.')
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

  test('Write it lists what to fix first, marks the line, and says why Save waits', async ({ page }) => {
    await openApp(page, '/new')
    const editor = page.getByRole('textbox', { name: 'Config contents' })
    await expect(editor).toContainText('[program:acme-queue]')
    await editor.click()
    await page.keyboard.press('ControlOrMeta+a')
    await page.keyboard.insertText('[program:e2e-task]\ncommand=/bin/sleep 600\ndirectory=/tmp\nnumprocs=2\nautostart=false\n')

    // The problem is listed above the program's card, with its line.
    const toFix = page.getByRole('group', { name: 'To fix before saving' })
    await expect(toFix).toContainText('Line 4')
    await expect(toFix).toContainText('process_name')

    // The bar at the bottom: a disabled Save, the reason as text next to it.
    const bar = page.getByTestId('save-bar')
    await expect(bar.getByRole('button', { name: 'Save & apply' })).toBeDisabled()
    await expect(bar.getByRole('status')).toHaveText('Fix line 4 to save.')
    // The summary above stays neutral.
    await expect(page.getByTestId('explain-counts')).not.toContainText('can’t be saved')

    // The error line has an icon in the gutter, not only a tint.
    const icon = page.getByTestId('ini-editor').locator('.cm-mark-icon[data-mark="error"]')
    await expect(icon).toHaveCount(1)
    await expect(icon).toHaveAttribute('title', 'Error on this line')

    // Clicking the item puts the cursor on that line.
    await editor.click()
    await page.keyboard.press('ControlOrMeta+Home')
    await expect(page.locator('.cm-activeLine')).toHaveText('[program:e2e-task]')
    await toFix.getByRole('button', { name: /Line 4/ }).click()
    await expect(page.locator('.cm-activeLine')).toHaveText('numprocs=2')
    expect(existsSync(FILE)).toBe(false)
  })

  test('the form checks the template, counts what to check, and folds every line', async ({ page }) => {
    await openApp(page, '/new/form')
    const chips = page.getByRole('group', { name: 'Templates' })
    await chips.getByRole('button', { name: 'Any script' }).click()
    await expect(page.getByLabel('Program name')).toHaveValue('acme-task')
    // The chosen chip has a check mark, the others none.
    await expect(chips.getByRole('button', { name: 'Any script' })).toHaveAttribute('aria-pressed', 'true')
    await expect(chips.getByRole('button', { name: 'Any script' }).locator('svg.check')).toBeVisible()
    await expect(chips.locator('svg.check')).toHaveCount(1)

    // A missing working folder is one thing to check, linked to its field.
    await page.getByLabel('Command').fill('/bin/sleep 600')
    await page.getByLabel('Working folder').fill('/tmp/visor-e2e-no-such-folder')
    const checks = page.getByTestId('things-to-check')
    await expect(checks.locator('.th')).toHaveText(/^\d+ things? to check$/)
    await expect(checks).toContainText('Working folder')
    await checks.getByRole('button', { name: /Working folder/ }).click()
    await expect(page.getByLabel('Working folder')).toBeFocused()
    // The preview marks that line too.
    await expect(page.getByLabel('File contents').locator('.pline.warning')).toContainText('directory=/tmp/visor-e2e-no-such-folder')

    // Every line is explained in the program's card, which folds. The only
    // card starts open; a new file has no live state.
    const lines = page.getByRole('region', { name: 'What this config does' })
    const card = lines.getByRole('button', { name: /^Program acme-task/ })
    await expect(card).toHaveAttribute('aria-expanded', 'true')
    await expect(lines).toContainText('Runs it inside')
    await expect(card.locator('.state')).toHaveCount(0)
    await card.click()
    await expect(card).toHaveAttribute('aria-expanded', 'false')
    await expect(lines).not.toContainText('Runs it inside')
    await card.click()
    await expect(lines).toContainText('Runs it inside')

    // The same bar as Write it.
    await expect(page.getByTestId('save-bar').getByRole('button', { name: 'Save & apply' })).toBeVisible()
  })

  test('copies an existing config under new names and saves it like any new one', async ({ page }) => {
    await openApp(page, '/new')
    await expect(page.getByRole('textbox', { name: 'Config contents' })).toContainText('[program:acme-queue]')
    await page.getByRole('button', { name: 'Copy an existing config…' }).click()
    const picker = page.getByRole('dialog', { name: 'Copy an existing config' })
    await picker.getByRole('button', { name: /^mailer\.ini/ }).click()
    await expect(picker).toBeHidden()

    // Write it, prefilled with the copy under free names, and a note of the renames.
    const editor = page.getByRole('textbox', { name: 'Config contents' })
    await expect(editor).toContainText('[program:mailer-worker-copy]')
    await expect(editor).toContainText('[program:mailer-digest-copy]')
    await expect(page.getByLabel('File name')).toHaveValue('mailer-copy.ini')
    const note = page.getByTestId('copy-note')
    await expect(note).toContainText('Copied from mailer.ini')
    await expect(note).toContainText('mailer-worker → mailer-worker-copy')
    await expect(note).toContainText('mailer-digest → mailer-digest-copy')
    // mailer.ini names nothing of the original outside its sections.
    await expect(page.getByTestId('copy-doubts')).toHaveCount(0)
    expect(existsSync(COPY)).toBe(false) // nothing is written before Save

    const save = page.getByTestId('save-new')
    await expect(save).toBeEnabled()
    await save.click()
    await expect(page.getByRole('region', { name: 'Saved and applied' })).toContainText('mailer-worker-copy')
    expect(readFileSync(COPY, 'utf8')).toContain('[program:mailer-digest-copy]')

    // The dashboard lists the copy with both programs; the original is untouched.
    await openApp(page, '/')
    await waitForDashboard(page)
    await expect(configRow(page, 'mailer-copy.ini')).toBeVisible()
    await expect(processRow(page, 'mailer-worker-copy:mailer-worker-copy_00')).toBeVisible()
    await expect(processRow(page, 'mailer-digest-copy:mailer-digest-copy')).toBeVisible()
    await expect(configRow(page, 'mailer.ini')).toBeVisible()
  })

  test('opens with the New Config shortcut', async ({ page }) => {
    await waitForDashboard(page)
    await page.keyboard.press(`${words.mod}+n`)
    await expect(page).toHaveURL(/#\/new$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Add a new config' })).toBeVisible()
  })
})
