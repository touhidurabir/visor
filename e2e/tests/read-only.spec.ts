import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import { downConfig, startVisor, stopSupervisord, type Visor } from './helpers'

// A config folder Visor can't write (as when root owns a Linux package's
// /etc/supervisor/conf.d): its own supervisord on a config whose conf.d is
// chmod 0555, and its own Visor (words as on Linux).
test.describe('a read-only config folder', () => {
  let conf = ''
  let dir = ''
  let visor: Visor | undefined

  test.beforeAll(async () => {
    conf = downConfig('readonly-conf')
    dir = path.join(path.dirname(conf), 'conf.d')
    fs.writeFileSync(path.join(dir, 'sleeper.ini'), '[program:sleeper]\ncommand=sleep 3600\nautostart=true\n')
    execFileSync('supervisord', ['-c', conf])
    await expect(() => expect(fs.existsSync(path.join(path.dirname(conf), 's.sock'))).toBe(true)).toPass({ timeout: 15_000 })
    fs.chmodSync(dir, 0o555)
    visor = await startVisor('readonly', 8781, conf, true)
  })

  test.afterAll(async () => {
    await visor?.stop()
    if (dir) fs.chmodSync(dir, 0o755)
    if (conf) await stopSupervisord(conf)
  })

  test('Programs pages say so, and the writing actions are off with the reason', async ({ page }) => {
    await page.goto(visor!.url + '/#/')
    await expect(page.getByRole('link', { name: 'sleeper.ini', exact: true })).toBeVisible({ timeout: 15_000 })
    const banner = page.getByTestId('read-only-banner')
    await expect(banner).toContainText(`Read-only: ${dir}`)
    await banner.getByRole('link', { name: 'How to allow editing' }).click()
    await expect(page).toHaveURL(/#\/doctor$/)

    // Remove from supervisor moves the file, so it is off.
    await page.goto(visor!.url + '/#/')
    await page.getByRole('button', { name: 'Actions for sleeper.ini' }).click()
    const remove = page.getByRole('menuitem', { name: /^Remove from supervisor/ })
    await expect(remove).toBeDisabled()
    await expect(remove).toHaveAttribute('title', new RegExp(`^Read-only: ${dir}`))
    await page.keyboard.press('Escape')

    // Add new: the banner, and Save waits with the reason.
    await page.goto(visor!.url + '/#/new')
    await expect(page.getByTestId('read-only-banner')).toBeVisible()
    await expect(page.getByTestId('save-new')).toBeDisabled()
    await expect(page.getByText(new RegExp(`Read-only: ${dir}.*\\.`)).last()).toBeVisible()
  })

  test('Doctor gives the Linux fix: let the supervisor group write the folder', async ({ page }) => {
    await page.goto(visor!.url + '/#/doctor')
    await expect(page.getByRole('heading', { level: 1, name: 'Visor can watch, but not edit' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('heading', { name: 'To add and edit configs, let a group you’re in write the folder' })).toBeVisible()
    // Not in the group yet (the server build's facts): join it, let it write, log in again.
    const steps = page.getByTestId('folder-fix').getByRole('listitem')
    await expect(steps).toHaveCount(3)
    await expect(steps.nth(0).locator('code')).toHaveText(/^sudo groupadd -f supervisor && sudo usermod -aG supervisor \S+$/)
    await expect(steps.nth(1).locator('code')).toHaveText(`sudo chgrp supervisor ${dir} && sudo chmod g+w ${dir}`)
    await expect(steps.nth(2)).toContainText('Log out and back in')
  })
})
