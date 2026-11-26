import { expect, test, type Page } from '@playwright/test'
import { collectErrors, mcpCall, McpHttpError, mcpToken, MCP_URL, openApp, words } from './helpers'

// The MCP server page (M9) and real MCP calls against the e2e Visor, the way
// Claude Code or Codex make them. One Visor for every step, so in order.
test.describe.configure({ mode: 'serial' })

const LOCAL = words.local // "This computer" (the shared Visor words things as on Linux)

function access(page: Page) {
  return page.getByTestId('mcp-access')
}

async function setLevel(page: Page, label: string) {
  await access(page).getByLabel(`Access for ${LOCAL}`).selectOption({ label })
  await expect(access(page).getByLabel(`Access for ${LOCAL}`)).toHaveValue(label === 'Read' ? 'read' : label === 'Control' ? 'control' : label === 'Off' ? 'off' : 'configs')
}

test.describe('MCP server', () => {
  test('a wide window pairs the settings cards; a narrow one stacks them', async ({ page }) => {
    const top = async (name: string) => (await page.getByRole('group', { name }).boundingBox())!.y
    await page.setViewportSize({ width: 1900, height: 1100 })
    await openApp(page, '/mcp')
    expect(await top('Connecting')).toBe(await top('Configuration'))
    expect(await top('Privacy')).toBe(await top('Access'))
    await page.setViewportSize({ width: 1000, height: 900 })
    await expect.poll(() => top('Connecting')).toBeGreaterThan(await top('Configuration'))
  })

  test('the page starts Off; turning it on shows Running and commands with the token', async ({ page }) => {
    const errors = collectErrors(page)
    await openApp(page, '/')
    await page.getByRole('navigation', { name: 'Servers' }).getByRole('link', { name: 'MCP server' }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'MCP server' })).toBeVisible()
    await expect(page.getByTestId('mcp-status')).toHaveText('Off')

    await page.getByRole('switch', { name: 'Enable MCP server' }).check()
    await expect(page.getByTestId('mcp-status')).toHaveText('Running')
    const token = await mcpToken(page)
    expect(token).toMatch(/^vsr_[\w-]{40,}$/)

    const snippets = page.getByTestId('mcp-snippets')
    await expect(snippets).toContainText(`claude mcp add --scope user --transport http visor ${MCP_URL}`)
    await expect(snippets).not.toContainText(token) // hidden until Show
    await page.getByRole('button', { name: 'Show' }).click()
    await expect(snippets).toContainText(`Authorization: Bearer ${token}`)
    await page.getByRole('tab', { name: 'Codex' }).click()
    await expect(snippets).toContainText('--bearer-token-env-var VISOR_TOKEN')
    await expect(snippets).toContainText('[mcp_servers.visor]')
    await page.getByRole('tab', { name: /JSON/ }).click()
    await expect(snippets).toContainText('"mcpServers"')
    // A tablist: arrow keys move between the tools, the panel is named by its tab.
    await page.getByRole('tab', { name: /JSON/ }).focus()
    await page.keyboard.press('ArrowRight')
    await expect(page.getByRole('tab', { name: 'Claude Code' })).toBeFocused()
    await expect(page.getByRole('tab', { name: 'Claude Code' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tabpanel', { name: 'Claude Code' })).toContainText('claude mcp add')
    await page.keyboard.press('ArrowLeft')
    await expect(page.getByRole('tab', { name: /JSON/ })).toBeFocused()

    await expect(page.getByTestId('mcp-tools').locator('tbody td.mono')).toHaveCount(26)
    await expect(access(page).getByLabel(`Access for ${LOCAL}`)).toHaveValue('control')
    expect(errors).toEqual([])
  })

  test('a real MCP call lists the sandbox and shows in Recent calls', async ({ page }) => {
    await openApp(page, '/mcp')
    const token = await mcpToken(page)
    const res = await mcpCall(token, 'list_processes')
    expect(res.isError).toBe(false)
    expect(res.text).toContain('notes-indexer')
    const calls = page.getByTestId('mcp-calls')
    await expect(calls.locator('tbody tr').first()).toContainText('list_processes')
    await expect(calls.locator('tbody tr').first()).toContainText('e2e-test')
    await expect(calls.locator('tbody tr').first()).toContainText('OK')

    const status = await mcpCall(token, 'get_status')
    expect(status.text).toContain('"Reachable":true')
  })

  test('a wrong token, or a request from a web page, is refused', async ({ page }) => {
    await openApp(page, '/mcp')
    await expect(mcpCall('wrong', 'get_status')).rejects.toMatchObject({ status: 401 })
    const res = await fetch(MCP_URL, { method: 'POST', headers: { Origin: 'http://evil.example', Authorization: `Bearer ${await mcpToken(page)}` }, body: '{}' })
    expect(res.status).toBe(403)
  })

  test('Regenerate makes a new token and the old one stops working', async ({ page }) => {
    await openApp(page, '/mcp')
    const old = await mcpToken(page)
    await expect(page.getByTestId('mcp-regen-hint')).toContainText('copy the new command or config below into your other AI tools')
    await expect(page.getByRole('button', { name: 'Regenerate…' })).toHaveAccessibleDescription(/After a new token/)
    await page.getByRole('button', { name: 'Regenerate…' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Make a new token' }).click()
    await expect.poll(() => mcpToken(page)).not.toBe(old)
    const err = await mcpCall(old, 'get_status').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(McpHttpError)
    expect((await mcpCall(await mcpToken(page), 'get_status')).isError).toBe(false)
  })

  test('a machine set to Read refuses changes with the way to allow them', async ({ page }) => {
    await openApp(page, '/mcp')
    await setLevel(page, 'Read')
    const res = await mcpCall(await mcpToken(page), 'restart', { target: 'notes-indexer' })
    expect(res).toEqual({ isError: true, text: `${LOCAL} is set to Read for MCP. Raise it on Visor's MCP page.` })
    await expect(page.getByTestId('mcp-calls').locator('tbody tr').first()).toContainText('Refused')
    await setLevel(page, 'Control')
  })

  test('KILL always asks, without Allow for 10 minutes; focus stays in the question; Esc denies', async ({ page }) => {
    await openApp(page, '/mcp')
    const token = await mcpToken(page)
    const killed = mcpCall(token, 'signal_process', { name: 'reports-export', signal: 'KILL' })
    const prompt = page.getByTestId('mcp-ask')
    await expect(prompt).toContainText(`e2e-test wants to send KILL to reports-export on ${LOCAL}`)
    await expect(prompt.getByRole('button', { name: 'Allow for 10 minutes' })).toHaveCount(0)
    const deny = prompt.getByRole('button', { name: 'Deny' })
    const allow = prompt.getByRole('button', { name: 'Allow', exact: true })
    await expect(deny).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(allow).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(deny).toBeFocused()
    await page.keyboard.press('Escape')
    expect(await killed).toEqual({ isError: true, text: 'The person declined in Visor.' })
    await expect(prompt).toHaveCount(0)
  })

  test('asking: Deny declines; Allow restarts, and Activity says via the AI tool', async ({ page }) => {
    await openApp(page, '/mcp')
    const ask = access(page).getByRole('switch', { name: `Ask before changes on ${LOCAL}` })
    await ask.check()
    await expect(ask).toBeChecked()
    const token = await mcpToken(page)

    const denied = mcpCall(token, 'restart', { target: 'reports-export' })
    const prompt = page.getByTestId('mcp-ask')
    await expect(prompt).toContainText(`e2e-test wants to restart reports-export on ${LOCAL}`)
    await expect(prompt.getByRole('button', { name: 'Deny' })).toBeFocused()
    await prompt.getByRole('button', { name: 'Deny' }).click()
    expect(await denied).toEqual({ isError: true, text: 'The person declined in Visor.' })
    await expect(prompt).toHaveCount(0)

    const allowed = mcpCall(token, 'restart', { target: 'reports-export' })
    await expect(prompt).toBeVisible()
    await prompt.getByRole('button', { name: 'Allow', exact: true }).click()
    expect((await allowed).isError).toBe(false)

    await page.getByRole('navigation', { name: 'Servers' }).getByRole('link', { name: 'Activity' }).click()
    await expect(page.locator('[data-via="e2e-test"]').first()).toHaveText('via e2e-test', { timeout: 20_000 })

    // Back as it was for the other specs.
    await openApp(page, '/mcp')
    await access(page).getByRole('switch', { name: `Ask before changes on ${LOCAL}` }).uncheck()
    await page.getByRole('switch', { name: 'Enable MCP server' }).uncheck()
    await expect(page.getByTestId('mcp-status')).toHaveText('Off')
  })
})
