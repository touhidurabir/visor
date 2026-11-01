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
    expect(box.width).toBe(360)
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

  test('scrolling the page closes an open menu', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 500 }) // short enough to scroll
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).click()
    await expect(page.getByRole('menu')).toBeVisible()
    const content = page.locator('main.content')
    expect(await content.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
    const before = await content.evaluate((el) => el.scrollTop)
    await content.evaluate((el) => el.scrollBy(0, 60))
    expect(await content.evaluate((el) => el.scrollTop)).not.toBe(before)
    await expect(page.getByRole('menu')).toBeHidden()
  })

  test('copying a command puts it on the clipboard', async ({ page }) => {
    // Record what the page writes: WebKit gives tests no clipboard to read.
    await page.addInitScript(() => {
      const w = window as unknown as { copied: string[] }
      w.copied = []
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: async (t: string) => void w.copied.push(t) },
      })
    })
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).click()
    await page.getByRole('button', { name: 'Copy command: supervisorctl restart shop-queue:shop-queue_00' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Copied' })).toBeVisible()
    expect(await page.evaluate(() => (window as unknown as { copied: string[] }).copied)).toEqual(['supervisorctl restart shop-queue:shop-queue_00'])
  })

  test('without a browser clipboard the server build says it could not copy', async ({ page }) => {
    await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }))
    await waitForDashboard(page)
    await page.getByRole('button', { name: 'Actions for shop-queue:shop-queue_00' }).click()
    await page.getByRole('button', { name: 'Copy command: supervisorctl restart shop-queue:shop-queue_00' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Can’t copy' })).toBeVisible()
    await expect(page.getByRole('status').filter({ hasText: /^Copied/ })).toHaveCount(0)
  })
})
