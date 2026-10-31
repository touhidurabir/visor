import { expect, test } from '@playwright/test'
import { collectErrors } from './helpers'

test.describe('navigation', () => {
  test('sidebar links reach every page without errors', async ({ page }) => {
    const errors = collectErrors(page)
    await page.goto('/')
    const nav = page.getByRole('navigation', { name: 'Servers' })

    await expect(page.getByRole('heading', { level: 1, name: 'Local' })).toBeVisible()

    await nav.getByRole('link', { name: /^Issues/ }).click()
    await expect(page).toHaveURL(/#\/issues$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Issues' })).toBeVisible()

    await nav.getByRole('link', { name: 'Settings' }).click()
    await expect(page).toHaveURL(/#\/settings$/)
    await expect(page.getByText('All changes saved')).toBeVisible()

    await nav.getByRole('link', { name: 'Dashboard' }).click()
    await expect(page).toHaveURL(/#\/$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Local' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'shop-api.ini', exact: true })).toBeVisible()

    expect(errors).toEqual([])
  })

  test('an unknown route falls back to the dashboard', async ({ page }) => {
    const errors = collectErrors(page)
    await page.goto('/#/definitely/not/a/page')
    await expect(page).toHaveURL(/#\/$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Local' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('the config page loads without errors', async ({ page }) => {
    const errors = collectErrors(page)
    await page.goto('/')
    await page.getByRole('link', { name: 'notes.ini', exact: true }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'notes.ini' })).toBeVisible()
    expect(errors).toEqual([])
  })
})
