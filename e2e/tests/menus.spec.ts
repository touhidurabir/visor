import { expect, test } from '@playwright/test'
import { waitForDashboard } from './helpers'

test.describe('action menus', () => {
  test('a lone process shows the short supervisorctl name', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for mailer-digest:mailer-digest' }).click()
    const menu = page.getByRole('menu', { name: 'Actions for mailer-digest:mailer-digest' })
    await expect(menu.locator('.cmd', { hasText: /^supervisorctl (start|stop) mailer-digest$/ })).toHaveCount(2)
    await expect(menu.getByText('mailer-digest:mailer-digest')).toHaveCount(0)
  })

  test('a numbered process in a group keeps group:name', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).click()
    const menu = page.getByRole('menu', { name: 'Actions for shop-queue:shop-queue_00' })
    await expect(menu.locator('.cmd', { hasText: 'supervisorctl restart shop-queue:shop-queue_00' })).toBeVisible()
  })

  test('labels stay on one line and the menu stays inside the window', async ({ page }) => {
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).click()
    const menu = page.getByRole('menu', { name: 'Actions for shop-queue:shop-queue_00' })
    await expect(menu).toBeVisible()
    const box = (await menu.boundingBox())!
    const vp = page.viewportSize()!
    expect(box.width).toBe(320)
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(vp.width)
    expect(box.y + box.height).toBeLessThanOrEqual(vp.height)
    for (const label of await menu.locator('.lbl').all()) {
      expect((await label.boundingBox())!.height).toBeLessThan(20)
    }
  })

  test('the keyboard still moves through the items and Escape closes', async ({ page }) => {
    await waitForDashboard(page)
    const trigger = page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' })
    await trigger.focus()
    await page.keyboard.press('ArrowDown')
    const items = page.getByRole('menuitem').and(page.locator(':not([disabled])'))
    await expect(items.first()).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await expect(items.nth(1)).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('menu')).toBeHidden()
    await expect(trigger).toBeFocused()
  })
})
