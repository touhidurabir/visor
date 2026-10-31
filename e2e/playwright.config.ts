import { defineConfig, devices } from '@playwright/test'

// The tests drive the real frontend through the Wails server-mode build
// (bin/visor-e2e) against a throwaway supervisord under /tmp/visor-e2e.
// WebKit is the engine closest to the macOS app's WKWebView.
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: 'http://127.0.0.1:8766',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'webkit', use: { ...devices['Desktop Safari'], viewport: { width: 1280, height: 800 } } }],
  webServer: {
    command: './scripts/serve.sh',
    cwd: import.meta.dirname,
    url: 'http://127.0.0.1:8766',
    reuseExistingServer: false,
    // serve.sh's trap removes the sandbox; give it time to do so.
    gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
    timeout: 60_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
