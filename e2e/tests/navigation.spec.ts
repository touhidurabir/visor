import { expect, test } from '@playwright/test'
import { collectErrors, words } from './helpers'

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

    await nav.getByRole('link', { name: 'Add new' }).click()
    await expect(page).toHaveURL(/#\/new$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Add a new config' })).toBeVisible()

    await nav.getByRole('link', { name: 'Doctor' }).click()
    await expect(page).toHaveURL(/#\/doctor$/)
    await expect(page.getByRole('heading', { level: 1, name: words.ready })).toBeVisible()

    expect(errors).toEqual([])
  })

  test('the sidebar marks the current page; servers open as tabs above it', async ({ page }) => {
    const errors = collectErrors(page)
    await page.goto('/#/issues')
    const nav = page.getByRole('navigation', { name: 'Servers' })
    await expect(nav.getByRole('link', { name: /^Issues/ })).toHaveClass(/\bon\b/)
    await expect(nav.getByRole('link', { name: 'Dashboard' })).not.toHaveClass(/\bon\b/)

    // A config's page belongs under Dashboard.
    await page.goto('/')
    await page.getByRole('link', { name: 'notes.ini', exact: true }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'notes.ini' })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Dashboard' })).toHaveClass(/\bon\b/)

    // Servers open as tabs: this computer's tab is first and selected, with + beside it.
    const tabs = page.getByRole('tablist', { name: 'Servers' })
    await expect(tabs.getByRole('tab', { name: /This computer/ })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: 'Open a server' })).toBeVisible()
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
