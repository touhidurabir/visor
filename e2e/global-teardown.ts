import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

// Playwright stops the web server first; this is the safety net that removes
// the sandbox (and any child it left) if serve.sh could not.
export default function globalTeardown() {
  execFileSync(fileURLToPath(new URL('./scripts/teardown.sh', import.meta.url)), { stdio: 'inherit' })
}
