import { existsSync, readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { collectErrors, configRow, expectState, openApp, removeConfig } from './helpers'

// Imports files into the sandbox's conf.d and takes them out again, so the
// 12 sample programs stay as the other specs expect them.
const DIR = '/tmp/visor-e2e/conf.d'
const A = `${DIR}/e2e-imp-a.ini`
const B = `${DIR}/e2e-imp-b.ini`
const A_TEXT = '[program:e2e-imp-a]\ncommand=/bin/sleep 600\nautostart=false\n'

test.beforeEach(async ({ page }) => {
  await openApp(page, '/')
  for (const f of [A, B]) await removeConfig(page, f)
})
test.afterEach(async ({ page }) => {
  for (const f of [A, B]) await removeConfig(page, f)
})

const card = (page: Page, source: string) => page.getByTestId(`import-file-${source}`)

/** Picks files with the page's file picker, as Choose files… would. */
async function pick(page: Page, files: { name: string; text: string }[]) {
  await page.getByTestId('import-picker').setInputFiles(files.map((f) => ({ name: f.name, mimeType: 'text/plain', buffer: Buffer.from(f.text) })))
}

test.describe('import config files', () => {
  test('checks picked files together, leaves one out, adds and starts the rest', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/new')
    await page.getByRole('link', { name: 'Import files' }).click()
    await expect(page.getByRole('link', { name: 'Import files' })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('heading', { name: 'Bring in configs you already have' })).toBeVisible()
    await expect(page.getByTestId('import-add')).toBeDisabled()

    await pick(page, [
      // .conf: renamed to .ini, which the sandbox's include setting loads.
      { name: 'e2e-imp-a.conf', text: A_TEXT },
      // The sandbox already has mailer-digest.
      { name: 'e2e-imp-b.ini', text: '[program:mailer-digest]\ncommand=/bin/sleep 600\n' },
      // The folder already has reports.ini.
      { name: 'reports.ini', text: '[program:e2e-imp-c]\ncommand=/bin/sleep 600\n' },
    ])

    const a = card(page, 'e2e-imp-a.conf')
    await expect(a.getByLabel('File name for e2e-imp-a.conf')).toHaveValue('e2e-imp-a.ini')
    await expect(a).toContainText('Renamed from e2e-imp-a.conf')
    await expect(a.getByTestId('import-status')).toHaveText('Ready')
    await expect(a).toContainText('Program: e2e-imp-a.')

    const b = card(page, 'e2e-imp-b.ini')
    await expect(b.getByTestId('import-status')).toHaveText('1 problem to fix')
    await expect(b).toContainText('[program:mailer-digest] already exists in mailer.ini')
    const c = card(page, 'reports.ini')
    await expect(c).toContainText(`reports.ini already exists in ${DIR}`)
    await expect(page.getByTestId('import-add')).toBeDisabled()
    await expect(page.getByRole('status').filter({ hasText: 'Fix 2 files to add' })).toBeVisible()

    // Fix b in its editor: another program name.
    await b.getByRole('button', { name: 'Line 1' }).click()
    const editor = b.getByRole('textbox', { name: 'Contents of e2e-imp-b.ini' })
    await expect(editor).toBeVisible()
    await editor.click()
    await page.keyboard.press('ControlOrMeta+a')
    await page.keyboard.insertText('[program:e2e-imp-b]\ncommand=/bin/sleep 600\n')
    await expect(b.getByTestId('import-status')).toHaveText('Ready')

    // Leave reports.ini out; a rename would do too.
    await c.getByLabel('Add reports.ini').uncheck()
    await expect(c.getByTestId('import-status')).toHaveText('Left out')
    const add = page.getByTestId('import-add')
    await expect(add).toHaveText('Add 2 configs')
    await expect(add).toBeEnabled()
    await expect(page.getByLabel('Start them after adding')).toBeChecked()
    await add.click()

    const results = page.getByTestId('import-results')
    await expect(results).toContainText('e2e-imp-a.ini · supervisord added e2e-imp-a and started it.', { timeout: 20_000 })
    await expect(results).toContainText('e2e-imp-b.ini · supervisord added e2e-imp-b and started it.')
    expect(readFileSync(A, 'utf8')).toBe(A_TEXT)
    expect(existsSync(B)).toBe(true)
    // Added files leave the list; the one left out stays.
    await expect(card(page, 'e2e-imp-a.conf')).toHaveCount(0)
    await expect(card(page, 'reports.ini')).toBeVisible()

    // autostart=false, yet running: Start them after adding started it.
    await openApp(page, '/')
    await expectState(page, 'e2e-imp-a:e2e-imp-a', 'Running')
    await expectState(page, 'e2e-imp-b:e2e-imp-b', 'Running')
    expect(errors).toEqual([])
  })

  test('two files in the batch can’t share a name or a program, and dropping files adds them', async ({ page }) => {
    await openApp(page, '/new/import')
    await expect(page.getByTestId('import-drop')).toBeVisible()
    // Drop two files on the page, as the Finder or a file manager would.
    await page.getByTestId('import-drop').evaluate((el) => {
      const dt = new DataTransfer()
      dt.items.add(new File(['[program:e2e-twin]\ncommand=/bin/sleep 600\n'], 'twin.ini'))
      dt.items.add(new File(['[program:e2e-twin]\ncommand=/bin/sleep 600\n'], 'twin.ini'))
      el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }))
    })
    const cards = page.locator('[data-testid="import-file-twin.ini"]')
    await expect(cards).toHaveCount(2)
    for (const i of [0, 1]) {
      await expect(cards.nth(i)).toContainText('Another file you’re importing is also named twin.ini')
      await expect(cards.nth(i)).toContainText('[program:e2e-twin] already exists in twin.ini')
    }
    // Removing one from the list clears both problems on the other.
    await cards.nth(1).getByRole('button', { name: 'Remove twin.ini from the list' }).click()
    await expect(cards).toHaveCount(1)
    await expect(cards.first().getByTestId('import-status')).toHaveText('Ready')
    await page.getByRole('link', { name: 'Cancel' }).click()
    await expect(page).toHaveURL(/#\/$/)
    await expect(configRow(page, 'twin.ini')).toHaveCount(0)
  })
})
