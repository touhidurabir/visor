import { beforeEach, describe, expect, it, vi } from 'vitest'

// The Wails clipboard, faked: in the server build its SetText resolves
// without copying anything, so Text() does not give the text back.
let wailsText = ''
let wailsCopies = true
const setText = vi.fn(async (t: string) => {
  if (wailsCopies) wailsText = t
})
vi.mock('@wailsio/runtime', () => ({ Clipboard: { SetText: (t: string) => setText(t), Text: async () => wailsText } }))

const { copyText } = await import('./clipboard')

describe('copyText', () => {
  beforeEach(() => {
    wailsText = ''
    wailsCopies = true
    setText.mockClear()
    vi.unstubAllGlobals()
  })

  it('uses the browser clipboard when it has one', async () => {
    const writeText = vi.fn(async () => {})
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    await copyText('ls -la')
    expect(writeText).toHaveBeenCalledWith('ls -la')
    expect(setText).not.toHaveBeenCalled()
  })

  it('falls back to the Wails clipboard when the browser one refuses', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: async () => Promise.reject(new Error('NotAllowedError')) } })
    await copyText('ls -la')
    expect(wailsText).toBe('ls -la')
  })

  it('uses the Wails clipboard when there is no browser one', async () => {
    vi.stubGlobal('navigator', {})
    await copyText('ls -la')
    expect(wailsText).toBe('ls -la')
  })

  it('fails when nothing was copied, so no "Copied" toast shows', async () => {
    vi.stubGlobal('navigator', {})
    wailsCopies = false
    await expect(copyText('ls -la')).rejects.toThrow(/copy/i)
  })
})
