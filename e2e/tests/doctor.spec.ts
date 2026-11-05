import { expect, test, type Page } from '@playwright/test'
import { collectErrors, downConfig, SANDBOX, startVisor, stopSupervisord } from './helpers'

// First-run Doctor needs a Visor that hasn't finished setup, so these tests
// start their own server-mode Visors (helpers.ts startVisor) beside the
// shared one.

async function stepStatuses(page: Page): Promise<string[]> {
  return page.getByRole('list', { name: 'What Visor checked' }).getByRole('listitem').evaluateAll((els) => els.map((e) => e.getAttribute('data-status') ?? ''))
}

// Each check says its status in words beside the mark (board 1).
async function stepWords(page: Page): Promise<string[]> {
  return page.getByRole('list', { name: 'What Visor checked' }).getByTestId('step-word').allInnerTexts()
}

test.describe('Doctor', () => {
  test('first launch: all found, then Open dashboard finishes setup', async ({ page }) => {
    const visor = await startVisor('fresh', 8771, `${SANDBOX}/supervisord.conf`)
    try {
      const errors = collectErrors(page)
      await page.goto(visor.url + '/#/')
      await expect(page.getByRole('heading', { name: 'Your Mac is ready' })).toBeVisible({ timeout: 15_000 })
      expect(await stepStatuses(page)).toEqual(['ok', 'ok', 'ok', 'ok', 'ok'])
      expect(await stepWords(page)).toEqual(['Done', 'Done', 'Done', 'Done', 'Done'])
      await expect(page.getByText('5 of 5 checks passed')).toBeVisible()
      await expect(page.getByText(/\d+ processes in \d+ configs/)).toBeVisible()
      // No sidebar on first launch.
      await expect(page.getByRole('link', { name: 'Issues' })).toHaveCount(0)
      // The server build can't open at login: that toggle is off and locked.
      await expect(page.getByRole('checkbox', { name: /Open Visor when I log in/ })).toBeDisabled()
      await expect(page.getByRole('checkbox', { name: 'Notify me when a watched process fails' })).toBeChecked()

      await page.getByRole('button', { name: 'Open dashboard' }).click()
      await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()
      await page.reload()
      await expect(page.getByText(/supervisord 4\.\d+/).first()).toBeVisible()
      await expect(page.getByRole('heading', { name: 'Your Mac is ready' })).toHaveCount(0)
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
    }
  })

  test('supervisord not running: Start supervisord starts it as you', async ({ page }) => {
    test.setTimeout(60_000)
    const conf = downConfig()
    const visor = await startVisor('down', 8772, conf)
    try {
      const errors = collectErrors(page)
      await page.goto(visor.url + '/#/')
      await expect(page.getByRole('heading', { name: 'supervisord isn’t running' })).toBeVisible({ timeout: 15_000 })
      expect(await stepStatuses(page)).toEqual(['ok', 'ok', 'bad', 'ok', 'wait'])
      expect(await stepWords(page)).toEqual(['Done', 'Done', 'Needs attention', 'Done', 'Waiting'])
      await expect(page.getByText('Nothing is listening on s.sock')).toBeVisible()
      await expect(page.getByText('1 thing to fix')).toBeVisible()
      await expect(page.getByText(`supervisord -c ${conf}`)).toBeVisible()
      // The root-socket help opens in place.
      await page.getByRole('button', { name: 'supervisord runs as root?' }).click()
      await expect(page.getByText('Run supervisord as yourself (recommended)')).toBeVisible()

      await page.getByRole('button', { name: 'Start supervisord' }).click()
      await expect(page.getByRole('heading', { name: 'Your Mac is ready' })).toBeVisible({ timeout: 20_000 })
      expect(await stepStatuses(page)).toEqual(['ok', 'ok', 'ok', 'ok', 'ok'])
      expect(await stepWords(page)).toEqual(['Done', 'Done', 'Done', 'Done', 'Done'])
      expect(errors).toEqual([])
    } finally {
      await visor.stop()
      await stopSupervisord(conf)
    }
  })

  test('supervisord won’t start: shows what it said and the end of its log', async ({ page }) => {
    test.setTimeout(60_000)
    const conf = downConfig('doctor-broken-conf', '\n[program:broken]\nautostart=true\n')
    const visor = await startVisor('broken', 8775, conf)
    try {
      await page.goto(visor.url + '/#/')
      await page.getByRole('button', { name: 'Start supervisord' }).click({ timeout: 15_000 })
      const failure = page.getByRole('alert').filter({ hasText: 'supervisord didn’t start' })
      await expect(failure).toBeVisible({ timeout: 20_000 })
      // supervisord's own words, not the command line or a tip.
      await expect(failure).toContainText('does not specify a command')
      await expect(page.getByRole('heading', { name: 'supervisord isn’t running' })).toBeVisible()
    } finally {
      await visor.stop()
      await stopSupervisord(conf)
    }
  })

  test('no config: shows where it looked, and VISOR_CONFIG locks the choice', async ({ page }) => {
    const missing = `${SANDBOX}/doctor-missing/supervisord.conf`
    const visor = await startVisor('noconf', 8773, missing)
    try {
      await page.goto(visor.url + '/#/')
      await expect(page.getByRole('heading', { name: 'Where is your config?' })).toBeVisible({ timeout: 15_000 })
      expect(await stepStatuses(page)).toEqual(['ok', 'bad', 'wait', 'wait', 'wait'])
      expect(await stepWords(page)).toEqual(['Done', 'Needs attention', 'Waiting', 'Waiting', 'Waiting'])
      await expect(page.getByLabel('Your supervisord.conf')).toHaveValue(missing)
      await expect(page.getByLabel('Your supervisord.conf')).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Choose…' })).toBeDisabled()
      await expect(page.getByText('Set by VISOR_CONFIG. Unset it to choose a file here.')).toBeVisible()
      await page.getByText('Where Visor looked').click()
      await expect(page.locator('.looked').getByText(missing)).toBeVisible()
    } finally {
      await visor.stop()
    }
  })

  test('after setup, a failed first connection opens Doctor beside the app', async ({ page }) => {
    const visor = await startVisor('down-onboarded', 8774, downConfig(), true)
    try {
      await page.goto(visor.url + '/#/')
      await expect(page).toHaveURL(/#\/doctor$/, { timeout: 15_000 })
      await expect(page.getByRole('heading', { name: 'supervisord isn’t running' })).toBeVisible()
      await expect(page.getByRole('link', { name: 'Issues' }).first()).toBeVisible()
      // The dashboard is still one click away.
      await page.getByRole('link', { name: 'Dashboard' }).first().click()
      await expect(page).toHaveURL(/#\/$/)
    } finally {
      await visor.stop()
    }
  })

  test('the Doctor page in the sidebar shows the checks beside the app', async ({ page }) => {
    const errors = collectErrors(page)
    await page.goto('/#/doctor')
    await expect(page.getByRole('heading', { name: 'Your Mac is ready' })).toBeVisible()
    expect(await stepWords(page)).toEqual(['Done', 'Done', 'Done', 'Done', 'Done'])
    await expect(page.getByRole('link', { name: 'Issues' }).first()).toBeVisible()
    // Setup is done, so no first-run toggles.
    await expect(page.getByRole('checkbox')).toHaveCount(0)
    await page.getByRole('button', { name: 'Open dashboard' }).click()
    await expect(page).toHaveURL(/#\/$/)
    expect(errors).toEqual([])
  })
})
