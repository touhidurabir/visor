import { expect, test } from '@playwright/test'

// The server build keeps passwords in an in-memory keychain, so these never
// touch the login Keychain.
test.describe('connection credentials', () => {
  test('a saved password shows as saved, can be changed, and Remove clears the username too', async ({ page }) => {
    await page.goto('/#/settings')
    await page.getByLabel('Username').fill('me')
    await page.getByLabel('Password', { exact: true }).fill('s3cret')
    await page.getByRole('button', { name: 'Save and reconnect' }).click()

    // Saved: a status line, not an empty box.
    await expect(page.getByTestId('password-saved')).toHaveText('Saved in your Keychain')
    await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0)

    // Change… brings the box back; Cancel puts the status line back.
    await page.getByRole('button', { name: 'Change…' }).click()
    await expect(page.getByLabel('Password', { exact: true })).toBeFocused()
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByTestId('password-saved')).toBeVisible()

    await page.getByRole('button', { name: 'Remove password' }).click()
    await expect(page.getByTestId('password-saved')).toBeHidden()
    await expect(page.getByLabel('Username')).toHaveValue('')
    await page.reload()
    await expect(page.getByLabel('Username')).toHaveValue('')
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('')
  })
})
