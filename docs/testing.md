# Testing Visor

Visor has four layers of tests. The first three run in CI; the last is a
checklist for the parts only a real macOS session can show.

## Go tests

```sh
go test ./...          # everything; integration tests start their own temp supervisord
go test -short ./...   # skips the integration tests
go test -race ./...    # what CI runs
```

Integration tests need `supervisord` on the `PATH` (`brew install supervisor`)
and skip themselves without it.

## Frontend unit tests

```sh
npm --prefix frontend run typecheck
npm --prefix frontend test
```

## End-to-end UI tests

Playwright drives the real frontend in WebKit (the engine closest to the
macOS WebView) through the Wails server-mode build, against a throwaway
supervisord with the 12 sample processes of `scripts/sandbox.sh`.

```sh
wails3 task test:e2e                                  # build, install WebKit, run everything
wails3 task test:e2e -- tests/issues.spec.ts          # one spec
wails3 task test:e2e -- --headed --debug              # watch it run
DEBUG_SERVER=1 wails3 task test:e2e                   # also show Visor's own log (always saved in e2e/visor-server.log)
```

The task builds the frontend, builds `bin/visor-e2e` with `-tags server`, and
runs Playwright. The sandbox lives in `/tmp/visor-e2e` (set with
`VISOR_SANDBOX_DIR`, which must look like `/tmp/visor-<name>`), so the dev
sandbox in `/tmp/visor-sandbox` is left alone. Visor's state goes to
`/tmp/visor-e2e/state`, and the server build keeps saved passwords in memory:
a test run never touches your real supervisord, `~/Library/Application
Support/Visor` or the Keychain. The sandbox is removed when the run ends,
including after a failure or Ctrl-C, and a sandbox left by a run that was killed outright is replaced at the next start. The binary refuses to start unless `VISOR_CONFIG` and `VISOR_STATE_DIR` are set.

What it covers (`e2e/tests`):

- `dashboard.spec.ts`: connection to supervisord 4.x, the config files, stat tiles, the search shortcut and filter.
- `process-actions.spec.ts`: start and stop from a process menu; Stop all asks first and Cancel changes nothing.
- `config-detail.spec.ts`: Overview flags a missing command, Logs follows the chosen process, History renders.
- `issues.spec.ts`: the FATAL issue appears, filters and counts, resolving, search.
- `issues-manage.spec.ts`: the stats strip and tabs, kind and date filters, bulk resolve, and deleting one or many resolved issues (each with Cancel leaving things unchanged); works on `flaky`'s issues only.
- `settings.spec.ts`: config fixed by `VISOR_CONFIG`, polling interval and a notification toggle persist across a reload.
- `navigation.spec.ts`: sidebar links, unknown routes, no console errors.

Assertions poll (`expect(...).toHaveText`, `toPass`), never sleep, because
the sample processes change state on their own (`flaky` dies every 20 s).

On failure Playwright keeps a trace: `npx --prefix e2e playwright show-trace e2e/test-results/<test>/trace.zip`.

## Manual native checklist

Server mode has no tray, popover, Dock or native dialogs, so check these by
hand on a Mac (`wails3 task dev`) before a release:

- [ ] Menu-bar icon shows OK, warning and error (badge) as processes change (stop one, break a command, let one go FATAL).
- [ ] Clicking the icon opens the popover; clicking elsewhere closes it; it opens over another Space and over a full-screen app.
- [ ] Right-click menu: Open Visor, Preferences…, Quit; Launch at login is disabled.
- [ ] Closing the main window hides it and the Dock icon; Visor stays in the menu bar; Open Visor brings both back.
- [ ] Cmd+Q quits and leaves the sandbox supervisord running.
- [ ] Reveal in Finder and Open in editor open the right file or folder.
- [ ] Logs tab: Save… shows the save dialog and writes the file.
- [ ] Settings: Choose… shows the file dialog. `task dev` sets `VISOR_CONFIG`, which disables Choose…, so run `scripts/sandbox.sh up && VISOR_STATE_DIR=/tmp/visor-sandbox/state wails3 dev -config ./build/config.yml` instead and choose `/tmp/visor-sandbox/supervisord.conf` (a saved path wins over auto-detection).
- [ ] Saving a username and password asks the Keychain once; remove the test entry with `security delete-generic-password -s dev.getvisor.visor -a <config path>`.
- [ ] Notifications (when they ship): one per event, none for unwatched files.
