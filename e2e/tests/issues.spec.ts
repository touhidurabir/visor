import { expect, test } from '@playwright/test'

test.describe('issues', () => {
  test('lists the FATAL mailer-digest issue and resolves it', async ({ page }) => {
    await page.goto('/#/issues')
    const rows = page.locator('.table .row:not(.head)')
    const open = page.getByRole('button', { name: /^Open \(\d+\)$/ })
    const resolved = page.getByRole('button', { name: /^Resolved \(\d+\)$/ })
    const all = page.getByRole('button', { name: 'All' })
    const count = async (b: typeof open) => Number((await b.innerText()).match(/\((\d+)\)/)![1])
    const digest = rows.filter({ hasText: 'mailer-digest' }).filter({ hasText: 'FATAL' })

    // The FATAL issue shows up under All (wait for the process to give up).
    await all.click()
    await expect(digest.first()).toBeVisible({ timeout: 30_000 })

    // Retry-safe: an earlier attempt may already have resolved it.
    await open.click()
    await expect(open).toHaveAttribute('aria-pressed', 'true')
    if ((await digest.count()) > 0) {
      const resolvedBefore = await count(resolved)
      await digest.first().getByRole('button', { name: 'Resolve' }).click()
      // Other processes (flaky) may open issues meanwhile, so only the
      // Resolved count and the row itself are exact.
      await expect(resolved).toHaveText(`Resolved (${resolvedBefore + 1})`)
      await expect(digest).toHaveCount(0)
    }

    await resolved.click()
    await expect(resolved).toHaveAttribute('aria-pressed', 'true')
    await expect(digest.first()).toBeVisible()

    await all.click()
    await expect(all).toHaveAttribute('aria-pressed', 'true')
    await expect(digest.first()).toBeVisible()
  })

  test('search narrows the list', async ({ page }) => {
    await page.goto('/#/issues')
    const rows = page.locator('.table .row:not(.head)')
    await page.getByRole('button', { name: 'All' }).click()
    await expect(rows.filter({ hasText: 'mailer-digest' }).first()).toBeVisible({ timeout: 30_000 })
    const search = page.getByRole('searchbox', { name: 'Search issues' })
    await search.fill('mailer-digest')
    await expect(page.getByText(/matching issues?/)).toBeVisible()
    await expect(rows.filter({ hasText: 'mailer-digest' }).first()).toBeVisible()
    await expect(rows.filter({ hasNotText: 'mailer-digest' })).toHaveCount(0)
    await search.fill('no-such-process-xyz')
    await expect(page.getByText('No issues match your filters.')).toBeVisible()
  })
})
