import { expect, test, type Page } from '@playwright/test'

// Selection, filters and delete on the Issues page. The shared sandbox opens
// a new `flaky` issue about every 20 s, so these specs work on the flaky
// rows only (never mailer-digest, which issues.spec.ts relies on) and take
// whatever number of them exists instead of assuming one.
test.describe.configure({ timeout: 120_000 })

const rowsOf = (page: Page) => page.locator('.table .row:not(.head)')
const flaky = (page: Page) => rowsOf(page).filter({ hasText: 'flaky' })

async function show(page: Page, status: 'Open' | 'Resolved' | 'All', search = 'flaky') {
  await page.goto('/#/issues')
  await page.getByRole('button', { name: new RegExp(`^${status}( \\(\\d+\\))?$`) }).click()
  await expect(page.getByRole('button', { name: new RegExp(`^${status}( \\(\\d+\\))?$`) })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('searchbox', { name: 'Search issues' }).fill(search)
  // The search applies after a short debounce, then the list reloads.
  if (search) await expect(page.getByText(/^\d+ matching issues?$/)).toBeVisible()
  await expect(page.locator('.table')).toHaveAttribute('aria-busy', 'false')
}

/** Makes sure at least one resolved flaky issue exists, resolving a new open one if needed. */
async function ensureResolvedFlaky(page: Page) {
  await show(page, 'Resolved')
  if ((await flaky(page).count()) > 0) return
  await show(page, 'Open')
  const row = flaky(page).first()
  await expect(row).toBeVisible({ timeout: 60_000 })
  await row.getByRole('button', { name: 'Resolve' }).click()
  await show(page, 'Resolved')
  await expect(flaky(page).first()).toBeVisible()
}

const dialog = (page: Page) => page.getByRole('alertdialog')

test.describe('issues page', () => {
  test('the summary adds up and the tabs are links', async ({ page }) => {
    await page.goto('/#/issues')
    const stats = page.getByRole('group', { name: 'Issue totals' })
    for (const label of ['open', 'resolved', 'in the last 24 hours', 'in 7 days', 'in 30 days']) {
      await expect(stats.getByText(label, { exact: true })).toBeVisible()
    }
    const value = async (label: string) => Number(await stats.locator('.part', { hasText: label }).locator('strong').innerText())
    // A new issue can land between two reads, so poll until the numbers agree.
    await expect(async () => {
      expect(await value('in 7 days')).toBeGreaterThanOrEqual(await value('in the last 24 hours'))
      expect(await value('in 30 days')).toBeGreaterThanOrEqual(await value('in 7 days'))
    }).toPass()

    await page.getByRole('link', { name: 'supervisord log' }).click()
    await expect(page).toHaveURL(/#\/issues\/log$/)
    await expect(page.getByRole('region', { name: 'supervisord log' })).toBeVisible()
    await page.getByRole('navigation', { name: 'Issues sections' }).getByRole('link', { name: 'Issues' }).click()
    await expect(page).toHaveURL(/#\/issues$/)
    await expect(stats).toBeVisible()

    // The All tab's total is open plus resolved.
    await page.getByRole('button', { name: 'All' }).click()
    await expect(async () => {
      const total = Number((await page.locator('.table .foot').innerText()).match(/of (\d+) issues/)![1])
      expect(total).toBe((await value('open')) + (await value('resolved')))
    }).toPass()
  })

  test('the kind filter keeps only that kind', async ({ page }) => {
    await show(page, 'All', '')
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible({ timeout: 30_000 })
    await page.getByLabel('Kind', { exact: true }).selectOption({ label: "Can't start" })
    await expect(rowsOf(page).first()).toBeVisible()
    await expect(rowsOf(page).locator('.kind').filter({ hasNotText: "Can't start" })).toHaveCount(0)
    await page.getByLabel('Kind', { exact: true }).selectOption({ label: 'supervisord unreachable' })
    await expect(rowsOf(page).locator('.kind').filter({ hasNotText: 'Unreachable' })).toHaveCount(0)
    await page.getByLabel('Kind', { exact: true }).selectOption({ label: 'All kinds' })
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible()
  })

  test('the date filter narrows by when an issue opened', async ({ page }) => {
    await show(page, 'All', '')
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible({ timeout: 30_000 })
    await page.getByLabel('Date', { exact: true }).selectOption({ label: 'Last 24 hours' })
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible()
    await page.getByLabel('Date', { exact: true }).selectOption({ label: 'Custom range…' })
    await page.getByLabel('From date').fill('2000-01-01')
    await page.getByLabel('To date').fill('2000-01-02')
    await expect(page.getByText('No issues match your filters.')).toBeVisible()
    await page.getByLabel('Date', { exact: true }).selectOption({ label: 'Any time' })
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible()
  })

  test('resolves the selected open issues after a confirmation', async ({ page }) => {
    await show(page, 'Open')
    await expect(flaky(page).first()).toBeVisible({ timeout: 60_000 })
    await page.getByRole('checkbox', { name: 'Select all loaded issues' }).check()
    const bar = page.getByRole('region', { name: 'Selected issues' })
    const resolve = bar.getByRole('button', { name: /^Resolve \d+$/ })
    await expect(resolve).toBeEnabled()
    const n = Number((await resolve.innerText()).match(/\d+/)![0])
    const issues = `${n} ${n === 1 ? 'issue' : 'issues'}`
    await expect(bar.getByRole('button', { name: 'Delete 0' })).toBeDisabled()

    // Cancel changes nothing.
    await resolve.click()
    await expect(dialog(page)).toContainText(`Resolve ${issues}?`)
    await expect(dialog(page)).toContainText("Visor won't reopen")
    await dialog(page).getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog(page)).toHaveCount(0)
    await expect(bar).toContainText(`${n} selected`)

    await resolve.click()
    // Resolving is not destructive: its button is not the red one.
    await expect(dialog(page).getByRole('button', { name: `Resolve ${issues}` })).not.toHaveClass(/danger/)
    await dialog(page).getByRole('button', { name: `Resolve ${issues}` }).click()
    await expect(page.getByText(`Resolved ${issues}`)).toBeVisible()
    await expect(bar).toHaveCount(0)
  })

  test('deletes one resolved issue from its row, and Cancel keeps it', async ({ page }) => {
    await ensureResolvedFlaky(page)
    const before = await flaky(page).count()
    const row = flaky(page).first()
    await expect(row.getByRole('button', { name: 'Resolve' })).toHaveCount(0)

    await row.getByRole('button', { name: 'Delete' }).click()
    await expect(dialog(page)).toContainText('Delete this issue?')
    await expect(dialog(page)).toContainText("This can't be undone.")
    await dialog(page).getByRole('button', { name: 'Cancel' }).click()
    await expect(flaky(page)).toHaveCount(before)

    await row.getByRole('button', { name: 'Delete' }).click()
    await expect(dialog(page).getByRole('button', { name: 'Delete issue' })).toHaveClass(/danger/)
    await dialog(page).getByRole('button', { name: 'Delete issue' }).click()
    await expect(page.getByText(/^Deleted 1 issue$/)).toBeVisible()
    await expect(flaky(page)).toHaveCount(before - 1)
  })

  test('deletes the selected resolved issues, never the open ones', async ({ page }) => {
    await ensureResolvedFlaky(page)
    // Under All, an open issue may sit among them: Delete counts resolved ones only.
    await show(page, 'All')
    await page.getByRole('checkbox', { name: 'Select all loaded issues' }).check()
    const bar = page.getByRole('region', { name: 'Selected issues' })
    const del = bar.getByRole('button', { name: /^Delete \d+$/ })
    await expect(del).toBeEnabled()
    const n = Number((await del.innerText()).match(/\d+/)![0])
    const issues = `${n} ${n === 1 ? 'issue' : 'issues'}`
    const picked = (await bar.innerText()).match(/(\d+) selected/)![1]

    await del.click()
    await expect(dialog(page)).toContainText(`Delete ${n} resolved ${n === 1 ? 'issue' : 'issues'}?`)
    await dialog(page).getByRole('button', { name: 'Cancel' }).click()
    await expect(bar).toContainText(`${picked} selected`)

    await del.click()
    await dialog(page).getByRole('button', { name: `Delete ${issues}` }).click()
    await expect(page.getByText(`Deleted ${issues}`)).toBeVisible()
    // An open issue that was selected too (flaky opens one every ~20 s)
    // stays selected; no resolved one does.
    await expect(bar.getByRole('button', { name: /^Delete [1-9]/ })).toHaveCount(0)
    if (await bar.count()) await bar.getByRole('button', { name: 'Clear selection' }).click()
    await expect(bar).toHaveCount(0)

    await show(page, 'Resolved')
    await expect(flaky(page)).toHaveCount(0)
    // mailer-digest was out of the search, so it is untouched.
    await show(page, 'All', 'mailer-digest')
    await expect(rowsOf(page).filter({ hasText: 'mailer-digest' }).first()).toBeVisible({ timeout: 30_000 })
  })

  test('filters live in the URL and survive a reload and a tab switch', async ({ page }) => {
    await page.goto('/#/issues')
    await page.getByRole('button', { name: /^Resolved\b/ }).click()
    await page.getByLabel('Kind', { exact: true }).selectOption({ label: "Can't start" })
    await page.getByLabel('Notified only').check()
    await page.getByRole('searchbox', { name: 'Search issues' }).fill('mailer')
    await expect(page).toHaveURL(/#\/issues\?s=resolved&k=fatal&q=mailer&n=1$/)
    // The server build never shows a notification, so nothing is notified.
    await expect(page.getByText('No issues match your filters.')).toBeVisible()

    const kept = async () => {
      await expect(page.getByRole('button', { name: /^Resolved\b/ })).toHaveAttribute('aria-pressed', 'true')
      await expect(page.getByLabel('Kind', { exact: true })).toHaveValue('fatal')
      await expect(page.getByLabel('Notified only')).toBeChecked()
      await expect(page.getByRole('searchbox', { name: 'Search issues' })).toHaveValue('mailer')
    }
    await page.reload()
    await kept()
    await page.getByRole('link', { name: 'supervisord log' }).click()
    await expect(page.getByRole('region', { name: 'supervisord log' })).toBeVisible()
    await page.getByRole('navigation', { name: 'Issues sections' }).getByRole('link', { name: 'Issues' }).click()
    await kept()

    await page.getByLabel('Notified only').uncheck()
    await expect(page).toHaveURL(/#\/issues\?s=resolved&k=fatal&q=mailer$/)
  })

  test('row actions fit the default window width', async ({ page }) => {
    await show(page, 'All', '')
    await expect(rowsOf(page).first()).toBeVisible({ timeout: 30_000 })
    const scroll = page.locator('.box .scroll')
    expect(await scroll.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(0)
    const box = (await scroll.boundingBox())!
    const btn = (await rowsOf(page).first().locator('.acts .btn').last().boundingBox())!
    expect(btn.x + btn.width).toBeLessThanOrEqual(box.x + box.width)
  })
})
