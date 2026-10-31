import { expect, test } from '@playwright/test'

test.describe('issues', () => {
  test('lists the FATAL mailer-digest issue and resolves it', async ({ page }) => {
    await page.goto('/#/issues')
    const rows = page.locator('.table .row:not(.head)')
    const digest = rows.filter({ hasText: 'mailer-digest' })
    await expect(digest.first()).toBeVisible({ timeout: 30_000 })
    await expect(digest.first()).toContainText('FATAL')

    const open = page.getByRole('button', { name: /^Open \(\d+\)$/ })
    const resolved = page.getByRole('button', { name: /^Resolved \(\d+\)$/ })
    const count = async (b: typeof open) => Number((await b.innerText()).match(/\((\d+)\)/)![1])
    await expect(open).toHaveAttribute('aria-pressed', 'true')
    const openBefore = await count(open)
    const resolvedBefore = await count(resolved)
    expect(openBefore).toBeGreaterThan(0)

    // The row with the mailer-digest FATAL issue.
    const target = rows.filter({ hasText: 'mailer-digest' }).filter({ hasText: 'FATAL' }).first()
    await target.getByRole('button', { name: 'Resolve' }).click()

    await expect(open).toHaveText(`Open (${openBefore - 1})`)
    await expect(resolved).toHaveText(`Resolved (${resolvedBefore + 1})`)

    await resolved.click()
    await expect(resolved).toHaveAttribute('aria-pressed', 'true')
    await expect(rows.filter({ hasText: 'mailer-digest' }).first()).toBeVisible()

    await page.getByRole('button', { name: 'All' }).click()
    await expect(page.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    await expect(rows.filter({ hasText: 'mailer-digest' }).first()).toBeVisible()
  })

  test('search narrows the list', async ({ page }) => {
    await page.goto('/#/issues')
    const rows = page.locator('.table .row:not(.head)')
    await page.getByRole('button', { name: 'All' }).click()
    await expect(rows.filter({ hasText: 'mailer-digest' }).first()).toBeVisible({ timeout: 30_000 })
    const search = page.getByRole('searchbox', { name: 'Search issues' })
    await search.fill('mailer-digest')
    await expect(page.getByText(/matching issues?/)).toBeVisible()
    await expect(rows.filter({ hasNotText: 'mailer-digest' })).toHaveCount(0)
    await search.fill('no-such-process-xyz')
    await expect(page.getByText('No issues match your search.')).toBeVisible()
  })
})
