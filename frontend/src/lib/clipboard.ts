// Copies text: through the browser's clipboard when the page has one (server
// builds, dev in a browser, and the app's WebView when it allows it), else
// through the Wails clipboard runtime (the macOS app). It resolves only when
// the text really was copied, so callers can say "Copied" on success.
import { Clipboard } from '@wailsio/runtime'

export async function copyText(text: string): Promise<void> {
  const browser = typeof navigator !== 'undefined' ? navigator.clipboard : undefined
  if (browser?.writeText) {
    try {
      await browser.writeText(text)
      return
    } catch {
      // Refused (no user gesture, not allowed here): try the app's clipboard.
    }
  }
  // In the server build SetText succeeds without copying anything, so check
  // that the clipboard now holds the text.
  await Clipboard.SetText(text)
  if ((await Clipboard.Text()) !== text) throw new Error('Couldn’t copy to the clipboard')
}
