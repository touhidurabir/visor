// Finder and editor actions on a path (reveal, open a folder, open a log in
// the text editor). They change nothing in supervisord; a failure becomes
// an error toast in plain English.
import { ServerService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { describeError } from './faults'
import { showToast } from './toast'

async function finder(fn: () => Promise<unknown>) {
  try {
    await fn()
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

export const reveal = (p: string) => void finder(() => ServerService.Reveal(p))
export const openPath = (p: string) => void finder(() => ServerService.OpenInEditor(p))
export const openLog = (p: string) => void finder(() => ServerService.OpenLog(p))
