// Which OS Visor runs on, and the words that depend on it: ⌘ or Ctrl,
// Keychain or keyring, Finder or the file manager, menu bar or tray.
// Go's SystemService.Platform is the one source; main.ts loads it before
// the app mounts, so everything here can read it synchronously.
import { SystemService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { OS, type Info } from '../../bindings/github.com/touhidurabir/visor/internal/platform/models'

export type { Info as PlatformInfo }

export const MAC: Info = { os: OS.Darwin, mod: '⌘', secretStore: 'Keychain', fileManager: 'Finder', tray: 'menu bar', systemSettings: true, local: 'This Mac' }
export const LINUX: Info = { os: OS.Linux, mod: 'Ctrl', secretStore: 'keyring', fileManager: 'file manager', tray: 'tray', systemSettings: false, local: 'This computer' }

let info: Info = MAC

/** Asks Go once; keeps the macOS words if it can't answer. */
export async function loadPlatform(): Promise<void> {
  try {
    info = await SystemService.Platform()
  } catch {
    // keep the default
  }
}

/** The OS and its wording. */
export function platform(): Info {
  return info
}

export const isMac = () => info.os === OS.Darwin

/** For tests. */
export function setPlatform(p: Info) {
  info = p
}

/** "Reveal in Finder" / "Show in file manager": select a file in a window of the file browser. */
export const revealLabel = () => (isMac() ? 'Reveal in Finder' : 'Show in file manager')
/** "Open in Finder" / "Open in file manager": a folder. */
export const openFolderLabel = () => `Open in ${info.fileManager}`
/** "Show in Finder" / "Show in file manager": a log file. */
export const showFileLabel = () => `Show in ${info.fileManager}`
/** "Keychain" / "keyring", after "your". */
export const secretStore = () => info.secretStore
/** "menu bar" / "tray". */
export const trayName = () => info.tray
/** "menu-bar icon" / "tray icon". */
export const trayIcon = () => (isMac() ? 'menu-bar icon' : 'tray icon')
