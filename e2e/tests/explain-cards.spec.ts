import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import { SANDBOX, openApp } from './helpers'

// "What this config does" has one card per program (Mac feedback 1). These
// read mailer.ini in the sandbox: mailer-worker (2 copies, running, a
// suggestion) and mailer-digest (exits at once and goes FATAL). Nothing is
// changed, so the other specs see the sandbox as it was.
const configPage = (file: string, query = '') => `/config/${encodeURIComponent(`${SANDBOX}/conf.d/${file}`)}${query}`

/** The computed colour of a design token, as getComputedStyle reports colours. */
async function token(page: Page, name: string): Promise<string> {
  return page.evaluate((n) => {
    const el = document.createElement('div')
    el.style.color = `var(${n})`
    document.body.append(el)
    const c = getComputedStyle(el).color
    el.remove()
    return c
  }, name)
}

async function openMailer(page: Page, query = '') {
  await openApp(page, configPage('mailer.ini', query))
  const panel = page.getByRole('region', { name: 'What this config does' })
  const digest = panel.getByRole('button', { name: /^Program mailer-digest/ })
  const worker = panel.getByRole('button', { name: /^Program mailer-worker/ })
  // mailer-digest needs a few seconds of retries to give up.
  await expect(digest).toContainText('Fatal', { timeout: 20_000 })
  return { panel, digest, worker }
}

test.describe('explanation cards', () => {
  test('one card per program: the failing one starts open, the healthy one closed', async ({ page }) => {
    const { panel, digest, worker } = await openMailer(page)
    await expect(panel.getByTestId('explain-counts')).toHaveText('· 2 programs')

    await expect(digest).toHaveAttribute('aria-expanded', 'true')
    await expect(worker).toHaveAttribute('aria-expanded', 'false')

    // Each header: the name, the live state, what it does in one sentence,
    // and what supervisord says when it fails.
    const digestCard = panel.locator('[data-card="program:mailer-digest"]')
    await expect(digest.locator('.name')).toHaveText('mailer-digest')
    await expect(digestCard).toContainText('Runs a shell command and starts with supervisord.')
    await expect(digestCard.locator('.failure')).toHaveText(/^Can’t run: /)
    await expect(worker).toContainText('Running')
    await expect(panel.locator('[data-card="program:mailer-worker"]')).toContainText('Runs two copies of a shell command and starts with supervisord.')
    await expect(panel.locator('[data-card="program:mailer-worker"] .failure')).toHaveCount(0)

    // The open card lists its lines; the closed one doesn't.
    await expect(digestCard.getByRole('listitem').first()).toBeVisible()
    await expect(panel.locator('[data-card="program:mailer-worker"]').getByRole('listitem')).toHaveCount(0)

    // The button says what it controls.
    const controls = await worker.getAttribute('aria-controls')
    expect(controls).toBeTruthy()
    await expect(page.locator(`[id="${controls}"]`)).toHaveCount(1)
  })

  test('each card opens and closes on its own, with the mouse or the keyboard', async ({ page }) => {
    const { panel, digest, worker } = await openMailer(page)
    await panel.getByRole('button', { name: 'All lines' }).click()
    const workerCard = panel.locator('[data-card="program:mailer-worker"]')

    await worker.click()
    await expect(worker).toHaveAttribute('aria-expanded', 'true')
    await expect(workerCard).toContainText('numprocs=2')
    await expect(digest).toHaveAttribute('aria-expanded', 'true') // the other card is left as it was

    await worker.focus()
    await page.keyboard.press('Enter')
    await expect(worker).toHaveAttribute('aria-expanded', 'false')
    await expect(workerCard).not.toContainText('numprocs=2')
    await page.keyboard.press('Space')
    await expect(worker).toHaveAttribute('aria-expanded', 'true')

    // A click on the one-line summary toggles too.
    await workerCard.locator('.summary').click()
    await expect(worker).toHaveAttribute('aria-expanded', 'false')
  })

  test('Expand all and Collapse all', async ({ page }) => {
    const { panel, digest, worker } = await openMailer(page)
    await panel.getByRole('button', { name: 'Expand all' }).click()
    await expect(digest).toHaveAttribute('aria-expanded', 'true')
    await expect(worker).toHaveAttribute('aria-expanded', 'true')
    await panel.getByRole('button', { name: 'Collapse all' }).click()
    await expect(digest).toHaveAttribute('aria-expanded', 'false')
    await expect(worker).toHaveAttribute('aria-expanded', 'false')
    await expect(panel.getByRole('listitem')).toHaveCount(0)
  })

  test('“Caused by line” opens the card that has the line', async ({ page }) => {
    const { panel, digest } = await openMailer(page)
    await digest.click()
    await expect(digest).toHaveAttribute('aria-expanded', 'false')
    const caused = page.getByRole('region', { name: /Last error/ }).getByRole('button', { name: /^line \d+$/ })
    const n = (await caused.textContent())!.replace(/\D/g, '')
    await caused.click()
    await expect(digest).toHaveAttribute('aria-expanded', 'true')
    await expect(panel.locator(`[id="explain-line-${n}"]`)).toBeFocused()
  })

  test('the Config tab stacks each line over its meaning, with the key highlighted', async ({ page }) => {
    const { panel, digest, worker } = await openMailer(page, '?tab=config')
    await expect(page.getByRole('textbox', { name: 'Editor for mailer.ini' })).toBeVisible()
    // The same cards and open rule as the Overview, with the live state.
    await expect(digest).toHaveAttribute('aria-expanded', 'true')
    await expect(worker).toHaveAttribute('aria-expanded', 'false')
    await expect(worker).toContainText('Running')

    const row = panel.getByRole('listitem').filter({ hasText: 'command=sh -c' }).first()
    const code = row.locator('code.code')
    await expect(code).toContainText("command=sh -c 'echo boom >&2; exit 1'")
    const meaning = row.locator('.meaning')
    await expect(meaning).toContainText('What it runs')
    // Stacked: the line as written above what it means, both starting at the same place.
    const c = (await code.boundingBox())!
    const m = (await meaning.boundingBox())!
    expect(m.y).toBeGreaterThanOrEqual(c.y + c.height - 1)
    expect(Math.abs(m.x - c.x)).toBeLessThan(2)
    // The key stands apart from its value; the meaning is plain text.
    await expect(code.locator('.c-key')).toHaveText('command')
    await expect(code.locator('.c-value')).toHaveText("sh -c 'echo boom >&2; exit 1'")
    expect(await code.locator('.c-key').evaluate((el) => getComputedStyle(el).color)).toBe(await token(page, '--accent-text'))
    expect(await code.evaluate((el) => getComputedStyle(el).fontFamily)).not.toBe(await meaning.evaluate((el) => getComputedStyle(el).fontFamily))

    // The section header has its name highlighted.
    const head = panel.locator('[data-card="program:mailer-digest"] code.code').first()
    await expect(head).toHaveText('[program:mailer-digest]')
    await expect(head.locator('.c-name')).toHaveText('mailer-digest')

    // Go to line still works from a stacked line (the one supervisord's failure is on).
    const go = panel.locator('[data-card="program:mailer-digest"]').getByRole('button', { name: /^Go to line \d+$/ }).first()
    const n = Number((await go.textContent())!.replace(/\D/g, ''))
    await go.click()
    await expect(page.locator('.cm-activeLine')).toHaveText(readFileSync(`${SANDBOX}/conf.d/mailer.ini`, 'utf8').split('\n')[n - 1])
  })

  test('Add new shows a card per program, without a live state', async ({ page }) => {
    await openApp(page, '/new')
    const editor = page.getByRole('textbox', { name: 'Config contents' })
    await expect(editor).toContainText('[program:acme-queue]')
    await editor.click()
    await page.keyboard.press('ControlOrMeta+a')
    await page.keyboard.insertText('[program:e2e-one]\ncommand=/bin/sleep 600\ndirectory=/tmp\n\n[program:e2e-two]\ncommand=/bin/sleep 600\nnumprocs=2\n')

    const panel = page.getByRole('region', { name: 'What this config does' })
    const one = panel.getByRole('button', { name: /^Program e2e-one/ })
    const two = panel.getByRole('button', { name: /^Program e2e-two/ })
    await expect(panel.getByTestId('explain-counts')).toHaveText('· 2 programs')
    // e2e-two has 2 copies under one name: an error, so it is open; e2e-one isn't.
    await expect(two).toHaveAttribute('aria-expanded', 'true')
    await expect(one).toHaveAttribute('aria-expanded', 'false')
    await expect(panel.locator('[data-card="program:e2e-one"]')).toContainText('Runs the program at /bin/sleep and starts with supervisord.')
    await expect(panel.locator('.state')).toHaveCount(0)
    // The fix list above the cards still links to the line.
    await expect(page.getByRole('group', { name: 'To fix before saving' })).toContainText('Line 7')
  })
})
