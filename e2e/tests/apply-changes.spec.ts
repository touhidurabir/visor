import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, configRow, openApp, removeConfig, restoreConfig, supervisorCall, supervisorStates, waitForDashboard } from './helpers'

// Edits files in the sandbox's conf.d from outside the app (as an editor
// would) and checks Visor notices, shows what applying does, applies, and
// clears the banner. notes.ini is a program that runs 2 s and exits, so
// restarting it disturbs no other spec. Everything is put back afterwards.
const NOTES = '/tmp/visor-e2e/conf.d/notes.ini'
const NEW = '/tmp/visor-e2e/conf.d/e2e-applied.ini'
const original = readFileSync(NOTES, 'utf8')

const banner = (page: Page) => page.getByRole('status').filter({ hasText: 'changed on disk' })
// A new file supervisord doesn't know yet: the banner offers Load for it.
const notLoaded = (page: Page) => page.getByTestId('changed-banner').filter({ hasText: 'isn’t loaded' })

/** Puts notes.ini back and applies it, whatever state a failed test left. */
const restoreNotes = (page: Page) => restoreConfig(page, NOTES, original, true)

test.describe.configure({ timeout: 60_000 })

test.beforeEach(async ({ page }) => {
  await openApp(page, '/')
  await removeConfig(page, NEW)
  await restoreNotes(page)
})
test.afterEach(async ({ page }) => {
  await removeConfig(page, NEW)
  await restoreNotes(page)
})

test.describe('apply changes', () => {
  test('a file edited on disk shows the banner; applying it lists the change and clears the banner', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    await expect(banner(page)).toBeHidden()

    writeFileSync(NOTES, original.replace('sleep 2', 'sleep 3'))
    await expect(banner(page)).toContainText('notes.ini changed on disk')
    await expect(configRow(page, 'notes.ini')).toContainText('changed')

    await banner(page).getByRole('button', { name: 'Apply changes' }).click()
    const dialog = page.getByRole('alertdialog')
    await expect(dialog).toContainText('Changed: notes-indexer')
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(banner(page)).toBeVisible() // nothing was applied

    await banner(page).getByRole('button', { name: 'Apply changes' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Apply changes' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Applied: 1 restarted' })).toBeVisible()
    await expect(banner(page)).toBeHidden()
    await expect(configRow(page, 'notes.ini')).not.toContainText('changed')

    // Back to the original through the file's own menu.
    writeFileSync(NOTES, original)
    await expect(configRow(page, 'notes.ini')).toContainText('changed')
    await configRow(page, 'notes.ini').getByRole('button', { name: 'Actions for notes.ini' }).click()
    await page.getByRole('menuitem', { name: /^Apply changes/ }).click()
    await expect(page.getByRole('alertdialog')).toContainText('Changed: notes-indexer')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Apply changes' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Applied: 1 restarted' })).toBeVisible()
    await expect(banner(page)).toBeHidden()
    expect(errors).toEqual([])
  })

  test('a change applied outside Visor stops showing as changed; one still waiting says supervisord confirmed it', async ({ page }) => {
    await waitForDashboard(page)
    writeFileSync(NOTES, original.replace('sleep 2', 'sleep 3'))
    await expect(banner(page)).toBeVisible()

    const configTab = `/config/${encodeURIComponent(NOTES)}?tab=config`
    await openApp(page, configTab)
    const card = page.getByRole('region', { name: 'Changed on disk since supervisord last loaded it' })
    await expect(card.getByTestId('loaded-status')).toHaveText('Checked with supervisord: it hasn’t loaded these changes yet. Apply them to load them.')
    await expect(card.getByRole('button', { name: 'Apply changes' })).toBeVisible()

    // What supervisorctl update (or a deploy script) does, outside Visor.
    await supervisorCall('supervisor.reloadConfig')
    await supervisorCall('supervisor.stopProcessGroup', 'notes-indexer')
    await supervisorCall('supervisor.removeProcessGroup', 'notes-indexer')
    await supervisorCall('supervisor.addProcessGroup', 'notes-indexer')

    await openApp(page, configTab) // opening the Config tab asks supervisord again
    await expect(page.getByRole('tab', { name: 'Config', selected: true })).toBeVisible()
    await expect(card).toHaveCount(0)
    await openApp(page, '/')
    await expect(banner(page)).toBeHidden()
    await expect(configRow(page, 'notes.ini')).not.toContainText('changed')
  })

  test('the server menu applies everything that differs', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Server actions' }).click()
    await page.getByRole('menuitem', { name: /^Apply all changes/ }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Nothing to apply' })).toBeVisible()

    writeFileSync(NOTES, original.replace('sleep 2', 'sleep 4'))
    await expect(banner(page)).toBeVisible()
    await page.getByRole('button', { name: 'Server actions' }).click()
    await page.getByRole('menuitem', { name: /^Apply all changes/ }).click()
    await expect(page.getByRole('alertdialog')).toContainText('Changed: notes-indexer')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Apply changes' }).click()
    await expect(banner(page)).toBeHidden()
  })

  test('a new file is added by Load, and Remove from supervisor takes it out and can be undone', async ({ page }) => {
    await waitForDashboard(page)
    writeFileSync(NEW, '[program:e2e-applied]\ncommand=/bin/sleep 600\ndirectory=/tmp\n')
    await expect(notLoaded(page)).toContainText('e2e-applied.ini isn’t loaded')
    // Load adds just that file's new programs, without a confirm.
    await notLoaded(page).getByRole('button', { name: 'Load' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Loaded e2e-applied.' })).toBeVisible()
    await expect.poll(async () => (await supervisorStates())['e2e-applied:e2e-applied'], { timeout: 15_000 }).toBe('RUNNING')

    const row = configRow(page, 'e2e-applied.ini')
    await row.getByRole('button', { name: 'Actions for e2e-applied.ini' }).click()
    await page.getByRole('menuitem', { name: /^Remove from supervisor/ }).click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText('moves to Visor’s backups')
    await confirm.getByRole('button', { name: 'Remove from supervisor' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Removed e2e-applied.ini from supervisor' })).toBeVisible()
    await expect(row).toBeHidden()
    expect(existsSync(NEW)).toBe(false)
    expect((await supervisorStates())['e2e-applied:e2e-applied']).toBeUndefined()

    await page.getByRole('button', { name: 'Undo' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Restored e2e-applied.ini' })).toBeVisible()
    await expect(configRow(page, 'e2e-applied.ini')).toBeVisible()
    expect(readFileSync(NEW, 'utf8')).toContain('[program:e2e-applied]')
    await expect.poll(async () => (await supervisorStates())['e2e-applied:e2e-applied'], { timeout: 15_000 }).toBe('RUNNING')
  })
})

test.describe('recently removed', () => {
  // I-5: once the report's Undo is gone (another page, a reload), the
  // dashboard still lists the removed file and restores it.
  test('a removed file can be restored from the dashboard after leaving it', async ({ page }) => {
    const errors = collectErrors(page)
    await waitForDashboard(page)
    writeFileSync(NEW, '[program:e2e-applied]\ncommand=/bin/sleep 600\ndirectory=/tmp\n')
    await expect(notLoaded(page)).toContainText('e2e-applied.ini isn’t loaded')
    await notLoaded(page).getByRole('button', { name: 'Load' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Loaded e2e-applied.' })).toBeVisible()
    await expect(configRow(page, 'e2e-applied.ini')).toBeVisible()
    await removeConfig(page, NEW)

    await page.getByRole('link', { name: /^Issues/ }).first().click()
    await expect(page.getByRole('heading', { level: 1, name: 'Issues' })).toBeVisible()
    await waitForDashboard(page)
    await expect(page.getByRole('button', { name: 'Undo' })).toBeHidden()
    const removed = page.getByRole('region', { name: 'Recently removed' })
    await expect(removed).toContainText('e2e-applied.ini')
    await removed.getByRole('button', { name: 'Restore e2e-applied.ini' }).click()
    await expect(page.getByText('Restored e2e-applied.ini.')).toBeVisible()
    await expect(configRow(page, 'e2e-applied.ini')).toBeVisible()
    expect(readFileSync(NEW, 'utf8')).toContain('[program:e2e-applied]')
    await expect.poll(async () => (await supervisorStates())['e2e-applied:e2e-applied'], { timeout: 15_000 }).toBe('RUNNING')
    await expect(removed.getByRole('button', { name: 'Restore e2e-applied.ini' })).toBeHidden()
    expect(errors).toEqual([])
  })
})

test.describe('replacing edits with a template', () => {
  test('asks before overwriting what was typed', async ({ page }) => {
    await openApp(page, '/new/form')
    await page.getByRole('button', { name: 'Any script' }).click()
    await expect(page.getByLabel('Program name')).toHaveValue('acme-task')
    // Untouched: switching template needs no question.
    await page.getByRole('button', { name: 'Node app' }).click()
    await expect(page.getByRole('alertdialog')).toBeHidden()
    await page.getByLabel('Command').fill('/bin/echo mine')
    await page.getByRole('button', { name: 'Any script' }).click()
    const dialog = page.getByRole('alertdialog')
    await expect(dialog).toContainText('Replace what you entered?')
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByLabel('Command')).toHaveValue('/bin/echo mine')
    await page.getByRole('button', { name: 'Any script' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Replace' }).click()
    await expect(page.getByLabel('Command')).not.toHaveValue('/bin/echo mine')
  })
})
