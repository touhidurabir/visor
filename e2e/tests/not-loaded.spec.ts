import { rmSync, writeFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { configRow, expectState, openApp, processRow, removeConfig } from './helpers'

// A config copied into conf.d by hand, which supervisord doesn't know until
// someone runs update: the dashboard lists it as not loaded, Load adds it,
// and Start loads it first. Taken out again afterwards.
const DIR = '/tmp/visor-e2e/conf.d'
const HAND = `${DIR}/e2e-by-hand.ini`
const HAND2 = `${DIR}/e2e-by-hand-2.ini`

test.beforeEach(async ({ page }) => {
  await openApp(page, '/')
  for (const f of [HAND, HAND2]) await removeConfig(page, f)
})
test.afterEach(async ({ page }) => {
  for (const f of [HAND, HAND2]) {
    // Not loaded: Remove from supervisor would refuse nothing, but a file
    // supervisord never knew can simply go.
    await removeConfig(page, f).catch(() => rmSync(f, { force: true }))
  }
})

test.describe('a config added outside Visor', () => {
  test('is listed as not loaded, and Start loads it first', async ({ page }) => {
    await openApp(page, '/')
    writeFileSync(HAND, '[program:e2e-by-hand]\ncommand=/bin/sleep 600\nautostart=false\n')
    const row = configRow(page, 'e2e-by-hand.ini')
    await expect(row).toContainText('1 program · not loaded', { timeout: 15_000 })
    await expect(row.getByRole('button', { name: 'Load e2e-by-hand.ini into supervisord' })).toBeVisible()
    await expect(page.locator('[data-not-loaded="e2e-by-hand"]')).toContainText('Not loaded')

    const banner = page.getByTestId('changed-banner')
    await expect(banner).toContainText('e2e-by-hand.ini isn’t loaded: supervisord doesn’t know its programs yet.')
    await expect(banner.getByRole('button', { name: 'Load' })).toBeVisible()

    // The menu says what Start does now.
    await row.getByRole('button', { name: 'Actions for e2e-by-hand.ini' }).click()
    await expect(page.getByRole('menuitem', { name: /^Stop all in file/ })).toBeDisabled()
    await page.getByRole('menuitem', { name: /^Load and start all in file/ }).click()
    await expectState(page, 'e2e-by-hand:e2e-by-hand', 'Running')
    await expect(page.locator('[data-not-loaded="e2e-by-hand"]')).toHaveCount(0)
    await expect(banner).toHaveCount(0)
    await expect(processRow(page, 'e2e-by-hand:e2e-by-hand')).toBeVisible()
  })

  test('the banner can be hidden until another file changes, and Load on the config page loads it', async ({ page }) => {
    await openApp(page, '/')
    writeFileSync(HAND, '[program:e2e-by-hand]\ncommand=/bin/sleep 600\n')
    const banner = page.getByTestId('changed-banner')
    await expect(banner).toContainText('e2e-by-hand.ini isn’t loaded', { timeout: 15_000 })
    await banner.getByRole('button', { name: 'Hide this until something else changes' }).click()
    await expect(banner).toHaveCount(0)
    // The row still says so.
    await expect(configRow(page, 'e2e-by-hand.ini')).toContainText('not loaded')

    // Another file: the banner is back, about both.
    writeFileSync(HAND2, '[program:e2e-by-hand-2]\ncommand=/bin/sleep 600\n')
    await expect(banner).toContainText('2 config files aren’t loaded', { timeout: 15_000 })
    await expect(banner.getByRole('button', { name: 'Apply changes' })).toBeVisible()

    await configRow(page, 'e2e-by-hand.ini').getByRole('link', { name: 'e2e-by-hand.ini' }).click()
    const note = page.getByTestId('not-loaded')
    await expect(note).toContainText('supervisord hasn’t loaded e2e-by-hand yet, so it runs nothing from this file.')
    await expect(page.getByRole('button', { name: 'Stop all' })).toBeDisabled()
    await note.getByRole('button', { name: 'Load' }).click()
    await expect(note).toHaveCount(0, { timeout: 15_000 })
    await openApp(page, '/')
    await expectState(page, 'e2e-by-hand:e2e-by-hand', 'Running')
  })
})

test('a file with a running program and a new one keeps its chips, and Load adds only the new one', async ({ page }) => {
  await openApp(page, '/')
  writeFileSync(HAND, '[program:e2e-by-hand]\ncommand=/bin/sleep 600\n')
  const row = configRow(page, 'e2e-by-hand.ini')
  await row.getByRole('button', { name: 'Load e2e-by-hand.ini into supervisord' }).click()
  await expectState(page, 'e2e-by-hand:e2e-by-hand', 'Running')

  // A second program added by hand: the first keeps running, the second waits.
  writeFileSync(HAND, '[program:e2e-by-hand]\ncommand=/bin/sleep 600\n\n[program:e2e-by-hand-b]\ncommand=/bin/sleep 600\n')
  await expect(page.locator('[data-not-loaded="e2e-by-hand-b"]')).toContainText('Not loaded', { timeout: 15_000 })
  await row.getByRole('link', { name: 'e2e-by-hand.ini' }).click()
  await expect(page.getByTestId('not-loaded')).toContainText('supervisord hasn’t loaded e2e-by-hand-b yet. Load adds it')
  await expect(page.getByRole('group', { name: 'Processes in this config' })).toContainText('e2e-by-hand')
  await expect(page.getByRole('button', { name: 'Stop all' })).toBeEnabled()
  await page.getByTestId('not-loaded').getByRole('button', { name: 'Load' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Loaded e2e-by-hand-b.' })).toBeVisible()
  await expect(page.getByTestId('not-loaded')).toHaveCount(0, { timeout: 15_000 })
})
