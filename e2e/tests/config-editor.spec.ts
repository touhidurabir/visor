import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, openApp, restoreConfig, supervisorStates } from './helpers'

// These edit notes.ini in the sandbox (a program that runs 2 s and exits, so
// restarting it disturbs no other spec), and put it back through the UI or,
// when a test fails halfway, from afterEach.
const FILE = '/tmp/visor-e2e/conf.d/notes.ini'
const original = readFileSync(FILE, 'utf8')

// Writing the file back is not enough after a test that failed past "Save &
// apply": supervisord would keep running the edit, and every later spec would
// see the "changed on disk" banner. restoreConfig applies it too.
test.afterEach(async ({ page }) => {
  await restoreConfig(page, FILE, original)
})

async function openEditor(page: Page) {
  await openApp(page, `/config/${encodeURIComponent(FILE)}?tab=config`)
  await expect(page.getByRole('heading', { level: 1, name: 'notes.ini' })).toBeVisible()
  const editor = page.getByRole('textbox', { name: 'Editor for notes.ini' })
  await expect(editor).toContainText('[program:notes-indexer]')
  return editor
}

/** Replaces the whole text of the editor. */
async function setText(page: Page, text: string) {
  const editor = page.getByRole('textbox', { name: 'Editor for notes.ini' })
  await editor.click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.insertText(text)
}

test.describe('config editor', () => {
  test('saves with a diff preview, applies only this file, and restores the backup', async ({ page }) => {
    const errors = collectErrors(page)
    await openEditor(page)
    const before = await supervisorStates()
    const save = page.getByRole('button', { name: 'Save & apply' })
    await expect(save).toBeDisabled() // nothing to save yet
    await expect(page.getByRole('region', { name: 'Line by line' })).toContainText('What it runs')
    await expect(page.getByRole('region', { name: 'Line by line' })).not.toContainText('Error.')

    await setText(page, original.replace('sleep 2', 'sleep 3'))
    await expect(page.getByText('Unsaved changes')).toBeVisible()
    await expect(save).toBeEnabled()
    await save.click()

    const preview = page.getByRole('dialog', { name: 'Review the change' })
    await expect(preview).toContainText('1 line added, 1 line removed')
    const diff = preview.getByRole('group', { name: 'Changes to be saved' })
    await expect(diff).toContainText('sleep 2')
    await expect(diff).toContainText('sleep 3')
    expect(readFileSync(FILE, 'utf8')).toBe(original) // nothing is written before the preview is confirmed
    await preview.getByRole('button', { name: 'Save & apply' }).click()

    const result = page.getByRole('region', { name: 'Saved and applied' })
    await expect(result).toContainText('Restarted with the new settings: notes-indexer')
    expect(readFileSync(FILE, 'utf8')).toContain('sleep 3')
    await expect(page.getByText('Unsaved changes')).toBeHidden()
    // Only this file's program was touched.
    const after = await supervisorStates()
    expect(after['shop-scheduler:shop-scheduler']).toBe(before['shop-scheduler:shop-scheduler'])

    // The backup holds the original; restoring it puts the file back.
    await page.getByRole('button', { name: 'Restore a backup…' }).click()
    const restore = page.getByRole('dialog', { name: /Restore a backup of notes.ini/ })
    await expect(restore.getByRole('option').first()).toBeVisible() // newest first
    await expect(restore.getByRole('group', { name: 'Changes from restoring this backup' })).toContainText('sleep 2')
    await restore.getByRole('button', { name: 'Restore this version' }).click()
    await expect(page.getByRole('region', { name: 'Restored and applied' })).toContainText('notes-indexer')
    expect(readFileSync(FILE, 'utf8')).toBe(original)
    expect(errors).toEqual([])
  })

  test('a comment-only edit saves without restarting anything', async ({ page }) => {
    await openEditor(page)
    await setText(page, `; reviewed by e2e\n${original}`)
    await page.getByRole('button', { name: 'Save & apply' }).click()
    await page.getByRole('dialog', { name: 'Review the change' }).getByRole('button', { name: 'Save & apply' }).click()
    await expect(page.getByRole('region', { name: 'Saved and applied' })).toContainText('Nothing needed to change')
    expect(readFileSync(FILE, 'utf8')).toBe(`; reviewed by e2e\n${original}`)

    await page.getByRole('button', { name: 'Restore a backup…' }).click()
    await page.getByRole('dialog', { name: /Restore a backup/ }).getByRole('button', { name: 'Restore this version' }).click()
    await expect(page.getByRole('region', { name: 'Restored and applied' })).toBeVisible()
    expect(readFileSync(FILE, 'utf8')).toBe(original)
  })

  test('an error blocks saving until its fix is applied', async ({ page }) => {
    await openEditor(page)
    await setText(page, '[program:notes-indexer]\ncommand=sleep 1\nnumprocs=2\n')
    const lines = page.getByRole('region', { name: 'Line by line' })
    await expect(lines).toContainText('Error.')
    await expect(lines).toContainText('process_name')
    const save = page.getByRole('button', { name: 'Save & apply' })
    await expect(save).toBeDisabled()
    await expect(save).toHaveAttribute('title', /Fix 1 line to save/)
    expect(readFileSync(FILE, 'utf8')).toBe(original)

    await lines.getByRole('button', { name: 'Apply fix' }).click()
    await expect(page.getByRole('textbox', { name: 'Editor for notes.ini' })).toContainText('process_name=%(program_name)s_%(process_num)02d')
    await expect(lines).not.toContainText('Error.')
    await expect(save).toBeEnabled()
    expect(readFileSync(FILE, 'utf8')).toBe(original) // a fix changes the text, not the file
  })

  test('a file edited elsewhere is not overwritten, and the edits stay until you choose', async ({ page }) => {
    await openEditor(page)
    await setText(page, `; my edit\n${original}`)
    const elsewhere = `${original}; someone else\n`
    writeFileSync(FILE, elsewhere)


    // Visor notices the other edit by itself, keeps yours, and blocks the
    // save (Go also refuses a stale save: TestConfigServiceSaveRejectsStaleHash).
    const banner = page.getByRole('alert').filter({ hasText: 'changed on disk after you opened it' })
    await expect(banner).toBeVisible()
    expect(readFileSync(FILE, 'utf8')).toBe(elsewhere)
    await expect(page.getByRole('textbox', { name: 'Editor for notes.ini' })).toContainText('; my edit')
    await expect(page.getByRole('button', { name: 'Save & apply' })).toBeDisabled()

    await banner.getByRole('button', { name: 'Review the differences' }).click()
    const compare = page.getByRole('dialog', { name: 'Your edits vs. the file on disk' })
    await expect(compare.getByRole('group', { name: 'File on disk compared with your edits' })).toContainText('someone else')
    await compare.getByRole('button', { name: 'Load the new version' }).click()
    await expect(page.getByRole('textbox', { name: 'Editor for notes.ini' })).toContainText('; someone else')
    await expect(page.getByRole('textbox', { name: 'Editor for notes.ini' })).not.toContainText('; my edit')
  })

  test('unsaved edits survive switching tabs', async ({ page }) => {
    await openEditor(page)
    await setText(page, `; draft\n${original}`)
    await page.getByRole('tab', { name: 'History' }).click()
    await page.getByRole('tab', { name: 'Config' }).click()
    await expect(page.getByRole('textbox', { name: 'Editor for notes.ini' })).toContainText('; draft')
    await expect(page.getByText('Your unsaved edits from earlier are back')).toBeVisible()
  })

  test('the overview explains the config line by line', async ({ page }) => {
    await openApp(page, `/config/${encodeURIComponent(FILE)}`)
    await expect(page.getByRole('region', { name: 'What this config does' })).toContainText('notes-indexer')
    const lines = page.getByRole('region', { name: 'Line by line' })
    // notes.ini has a suggestion, so the Overview opens on Only problems.
    await lines.getByRole('button', { name: 'All lines' }).click()
    await expect(lines).toContainText('[program:notes-indexer]')
    await expect(lines).toContainText('What it runs')
    // A missing setting shows the default that applies.
    await expect(lines).toContainText('Defaults apply')
  })
})
