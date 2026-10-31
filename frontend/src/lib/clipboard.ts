// Copies text: through the Wails clipboard runtime when it works (the macOS
// app), else the browser's clipboard (server builds and dev in a browser).
import { Clipboard } from '@wailsio/runtime'

export async function copyText(text: string): Promise<void> {
  try {
    await Clipboard.SetText(text)
  } catch {
    await navigator.clipboard.writeText(text)
  }
}
