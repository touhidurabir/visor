// Whether Visor may add, remove and restore configs: the include folder,
// and whether this user can write it (ConfigService.Folder). A folder root
// owns (supervisord installed from a Linux package) makes configs
// read-only: Programs pages say so and the writing actions are off. On a
// server it is the SSH user (root with Use sudo), and a server set to Read
// only is read-only whatever the permissions.
import { ref } from 'vue'
import { type FolderView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ConfigService } from './api'

const folder = ref<FolderView | null>(null)

/** Loads (or reloads) the folder's state; a failure leaves editing on. */
export async function loadFolder(): Promise<void> {
  try {
    folder.value = await ConfigService.Folder()
  } catch {
    folder.value = null
  }
}

/** The shared folder state, for views. */
export function useFolder() {
  return folder
}

/**
 * Why configs can't be written, as the banner and each disabled action's
 * tooltip say it; "" when they can.
 */
export function readOnlyReason(f: FolderView | null): string {
  if (!f || f.Writable || !f.Dir) return ''
  if (f.ServerReadOnly) return 'Read only: this server is set to Read only'
  if (f.Missing) return `Read-only: ${f.Dir} doesn’t exist${f.Remote ? ' on the server' : ''} yet`
  if (f.Remote) return `Read-only: ${f.User || 'the SSH user'} can’t write ${f.Dir} on the server`
  return f.Owner ? `Read-only: ${f.Dir} belongs to ${f.Owner}` : `Read-only: ${f.Dir} isn’t writable for you`
}

/** For tests. */
export function setFolder(f: FolderView | null) {
  folder.value = f
}
