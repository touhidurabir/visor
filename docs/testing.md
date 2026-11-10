# Testing Visor

Visor has four layers of tests. The first three run in CI; the last is a
checklist for the parts only a real macOS session can show.

How to start each kind of local run (dev, the sandbox, the packaged app, a
`.dmg`) is in [scripts.md](scripts.md).

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

The shared Visor runs with `VISOR_PLATFORM=linux` (only the server build reads it), so the UI words things as on Linux: Ctrl+N, "Show in file manager", "keyring", "tray". Specs take those words from `WORDS` in `e2e/tests/helpers.ts`; `platform.spec.ts` also starts a Visor with `VISOR_PLATFORM=darwin` on the same sandbox and checks the macOS words.

What it covers (`e2e/tests`):

- `dashboard.spec.ts`: connection to supervisord 4.x, the config files, the health strip and Show them, failing rows first, the search shortcut and filter, and the shutdown toast's Open Doctor (its own Visor on port 8776).
- `process-actions.spec.ts`: start and stop from a process menu; Stop all asks first and Cancel changes nothing.
- `config-detail.spec.ts`: Overview flags a missing command and opens on Only problems when there are any, Logs follows the chosen process, the header, History renders.
- `issues.spec.ts`: the FATAL issue appears, filters and counts, resolving, search.
- `issues-manage.spec.ts`: the summary line and tabs, kind and date filters, bulk resolve, and deleting one or many resolved issues (each with Cancel leaving things unchanged); works on `flaky`'s issues only.
- `platform.spec.ts`: the OS-dependent words on both platforms (shortcut hints and keys, the file-manager button, the tray count switch, the password store, launch at login, the macOS-only notification row), the macOS ones on its own Visor on port 8780.
- `settings.spec.ts`: config fixed by `VISOR_CONFIG`, the order of the cards and the label-left rows, polling interval and a notification toggle persist across a reload.
- `navigation.spec.ts`: sidebar links, unknown routes, no console errors.
- `config-editor.spec.ts`, `add-new.spec.ts`, `apply-changes.spec.ts`: editing with diff and backup, adding a config by form or by hand (the fix list and save bar), copying an existing config, applying changes made on disk, removing and restoring.
- `supervisord-log.spec.ts`: the supervisord log tab, its level filters, search and Follow.
- `menus.spec.ts`: process action menus, keyboard use, copying a command.
- `notifications.spec.ts`, `tray.spec.ts`: the bell and its unread count, the menu-bar popover (Needs attention, two-line rows with word buttons, copies folded into a "×N" row whose Restart restarts every copy, the footer and its Apply changes) and its Watch bells, a failing copy under Needs attention (its own Visor and supervisord on port 8779), and the popover with supervisord down, whose Open Doctor takes the main window to Doctor (its own Visor on port 8778).
- `theme.spec.ts`: System, Light and Dark (the switch is only in Settings), in the window and the popover.
- `connection.spec.ts`: a saved password shows as saved, can be changed and removed; with supervisord down, the dashboard and sidebar say so plainly (its own Visor on port 8777).
- `doctor.spec.ts`: first launch, Start supervisord (it really starts one), a config supervisord refuses, no config, Doctor opening when the first connection fails. It runs its own Visors on ports 8771–8775 with fresh state.

Assertions poll (`expect(...).toHaveText`, `toPass`), never sleep, because
the sample processes change state on their own (`flaky` dies every 20 s).

On failure Playwright keeps a trace: `npx --prefix e2e playwright show-trace e2e/test-results/<test>/trace.zip`.

## Manual native checklist

Server mode has no tray, popover, Dock or native dialogs, so check these by
hand on a Mac before a release, with `wails3 task dev`, or the packaged app
(`scripts/sign-check.sh adhoc` then `open`) where an item says so:

- [ ] App icon (packaged app): the Pulse icon (ring and mint pulse) in the Dock, Finder and the app switcher, not the Wails logo.
- [ ] Menu-bar icon: the ring and pulse, with no badge when all is well, a hollow badge for a warning and a filled badge for an error, as processes change (stop one, break a command, let one go FATAL). It reads in a light and a dark menu bar.
- [ ] Clicking the icon opens the popover; clicking elsewhere closes it; it opens over another Space and over a full-screen app.
- [ ] In the popover, failing processes are first, under Needs attention; each row has its name over its state and time, and only the buttons that apply. `mailer-worker ×2` unfolds to its two copies, and its Restart restarts both. Each config's bell is filled (watching) or crossed out (quiet); switching it there shows the same on the dashboard, and the header's bell opens the notifications in the main window. The footer fits on one row in the 360 px popover.
- [ ] Popover with supervisord down (shut it down from the dashboard): it says supervisord isn't running, and Open Doctor brings the main window forward on Doctor.
- [ ] Right-click menu: Open Visor, Preferences…, Launch at login, Quit. Launch at login is greyed out in `wails3 task dev` (no app bundle) and works in a packaged build; its check mark matches Settings → General.
- [ ] Closing the main window hides it and the Dock icon; Visor stays in the menu bar; Open Visor brings both back.
- [ ] Menu bar (at the top of the screen while the main window is in front): Visor, File, Edit, View, Supervisor, Window, Help. Check each item:
  - Preferences… (Cmd+,), Doctor, New Config… (Cmd+N), Dashboard (Cmd+1) and Issues (Cmd+2) open their pages.
  - Find (Cmd+F) focuses the dashboard search.
  - Reload Config (Cmd+R) checks for config changes once, never reloads the page; choose it from Issues and the dashboard opens and checks.
  - Restart All… asks first.
  - Close Window (Cmd+W) hides the window.
  - Copy and Paste work in the config editor.
  - Visor Help and Report an Issue open the browser, not the app window.
  - There is no "Reload" or "Learn More" anywhere.
- [ ] Cmd+Q quits and leaves the sandbox supervisord running.
- [ ] One Visor at a time (packaged app): with Visor running, open it again (`open -n` the same .app): no second menu-bar icon, the window comes forward. Turn launch at login on, then switch Visor off and on in System Settings → General → Login Items: still one Visor.
- [ ] Doctor, Start supervisord: shut the sandbox's supervisord down from the dashboard, relaunch, click Start supervisord: it starts and every step turns green. Break the config (add `[program:broken]` with no command to a sandbox .ini), shut down, Start supervisord again: "supervisord didn't start" with its error and log; remove the broken lines.
- [ ] Notifications: a banner's title starts with 🔴 (error: can't start, crash loop, supervisord not responding), 🟠 (warning: exited) or 🔵 (info), with no subtitle line, and its body is one sentence starting with the config file. Banners from one config stack together in Notification Centre. They stay until closed (Persistent); the bell's list shows the same severity icon before each title.
- [ ] Doctor, first launch: quit Visor, run it with a fresh state dir (`VISOR_STATE_DIR=/tmp/visor-sandbox/state-fresh`); the window opens on Doctor with no sidebar, the lens animates once, "Open dashboard" opens the app and a relaunch with the same state dir goes straight to the dashboard. In the packaged app, "Open Visor when I log in" is on and turns launch at login on.
- [ ] Doctor, Choose…: give it a saved path to a missing file, so it never auto-detects this Mac's supervisord: `mkdir -p /tmp/visor-sandbox/state-doc && printf '{"onboarded":true,"configPath":"/tmp/visor-sandbox/missing.conf"}' > /tmp/visor-sandbox/state-doc/settings.json`, then `VISOR_STATE_DIR=/tmp/visor-sandbox/state-doc wails3 dev -config ./build/config.yml` (no `VISOR_CONFIG`). Doctor says "Where is your config?"; Choose… shows the file dialog; picking a file that isn't a supervisord config says why it's refused; picking `/tmp/visor-sandbox/supervisord.conf` turns every step green.
- [ ] Reveal in Finder and Open in editor open the right file or folder.
- [ ] Logs tab: Save… shows the save dialog and writes the file.
- [ ] Settings: Choose… shows the file dialog. `task dev` sets `VISOR_CONFIG`, which disables Choose…, so run `scripts/sandbox.sh up && VISOR_STATE_DIR=/tmp/visor-sandbox/state wails3 dev -config ./build/config.yml` instead and choose `/tmp/visor-sandbox/supervisord.conf` (a saved path wins over auto-detection).
- [ ] Saving a username and password asks the Keychain once; remove the test entry with `security delete-generic-password -s dev.getvisor.visor -a <config path>`.
- [ ] Appearance: with the Mac set to Light, choose Dark in Settings → General (the sidebar no longer has the switch); the title bar, traffic-light area, scroll bars and the tray popover frame turn dark with the page; choose Light with the Mac on Dark; choose System and flip the Mac's appearance (System Settings → Appearance) while Visor is open: the window and popover follow. Quit and relaunch: the saved choice is already applied (the native part is the cgo `[NSApp setAppearance:]` in `internal/app/appearance_darwin.go`, which server mode can't show).
- [ ] Notifications. These work only in the packaged app: `wails3 task dev` and `go build` binaries have no bundle identifier, so Visor logs `visor: notifications are off: notifications require a valid bundle identifier` and Settings → Notifications says "Not available in this build" (the app still runs). To check them:
  1. `wails3 package` (builds `bin/visor.app`).
  2. `VISOR_SANDBOX_DIR=/tmp/visor-notify scripts/sandbox.sh up`
  3. `VISOR_CONFIG=/tmp/visor-notify/supervisord.conf VISOR_STATE_DIR=/tmp/visor-notify/state ./bin/visor.app/Contents/MacOS/visor`
  4. The first launch asks to allow notifications: click Allow. The terminal logs `visor: notification permission: allowed=true`.
  5. Within a few seconds `mailer-digest can't start` appears; `flaky exited unexpectedly` follows when flaky dies (about every 20 s, at most one a minute per process and kind). The Issues page shows the bell on each notified issue (`"notified":true` in `/tmp/visor-notify/state/issues.jsonl`).
  6. Click a notification: the main window comes to the front on Issues.
  7. Settings → Notifications shows "Allowed"; Send a test notification shows one. Turn Visor off in System Settings → Notifications: the page says so when you come back to it.
  8. Turn off "A process can't start" (or turn Watch off for mailer-digest's config), Resolve its issue and Start it again from Visor: it goes FATAL again with no notification. supervisord not responding notifies even when no config is watched.
  9. Quit Visor, then `VISOR_SANDBOX_DIR=/tmp/visor-notify scripts/sandbox.sh down`. macOS asks only once; change the answer later in System Settings → Notifications → Visor.

## Manual Linux checklist

Check these on Ubuntu 24.04 (GNOME, Wayland) and Fedora 40+ (KDE Plasma)
before a release. `scripts/linux-check.sh build` then `open` gives an
installed copy (the AppImage) against the sandbox; the items marked **.deb**
or **.rpm** need the package installed (`sudo apt install ./bin/visor.deb`,
`sudo dnf install ./bin/visor.rpm`), which is a real install.

- [ ] AppArmor (Ubuntu): the AppImage's first run stops before the window, prints the profile and the two `sudo` commands, saves them in `~/.config/Visor/apparmor-fix.txt` (the sandbox run: `/tmp/visor-linuxcheck/state/apparmor-fix.txt`) and shows a notification. After running them, it opens. The **.deb** opens straight away (`aa-status | grep dev.getvisor.visor`).
- [ ] Window: no in-window menu bar. The title bar follows Settings → Appearance; with System, switching the desktop between light and dark switches Visor live. The dock or task bar shows Visor's icon (**.deb**/**.rpm**: also in the app grid as Visor).
- [ ] Shortcuts: Ctrl+, Ctrl+N Ctrl+1 Ctrl+2 open their pages; Ctrl+F focuses the dashboard search and a log's search; Ctrl+R checks for config changes; Ctrl+W hides the window; Ctrl+Q quits; Ctrl+C copies selected log text. The hints say Ctrl, not ⌘.
- [ ] Tray icon: the coloured ring with the pulse; a badge with the unread count (9+ past nine) in red, orange or blue; a dot for a warning or an error with nothing unread; grey with no pulse before it connects. "Show the unread count on the tray icon" off: no number. The tooltip says Visor.
- [ ] Tray menu: Show status…, Open Visor, Preferences…, Notifications (N) with the same N as the bell, Launch at login, Quit. GNOME: a click opens this menu. KDE: a left click opens the popover.
- [ ] Popover: Show status… opens it and stays open when you click elsewhere; Esc, its close button or Show status… again close it. On Wayland it opens where the desktop puts it.
- [ ] Notifications: banners show Visor's name and icon (**.deb**/**.rpm**; a dev build shows dev.getvisor.visor), with the 🔴/🟠/🔵 title and a one-sentence body; a body with `&&` or `<…>` reads right. Clicking one opens Visor on Issues. Settings → Notifications says Allowed.
- [ ] Launch at login: on from the tray or Settings writes `~/.config/autostart/dev.getvisor.visor.desktop` (Exec is the AppImage file or `/usr/bin/visor`, with `--login`); log out and in: Visor starts quietly in the tray. An AppImage in `/tmp` can't turn it on and Doctor warns.
- [ ] One Visor: start the installed copy again: no second tray icon, the window comes forward. With `--login`: nothing comes forward.
- [ ] Files: Show in file manager opens Nautilus or Dolphin with the file selected; Open in file manager opens the folder; Open log in editor opens the default text editor and Visor doesn't wait for it to close.
- [ ] Keyring: saving a password in Settings → Connection says "Saved in your keyring" (GNOME Keyring, KWallet); remove it again. With the keyring locked or absent, saving says no keyring is running and the connection still works until you quit.
- [ ] Doctor with the distro's supervisord (**.deb**: `sudo apt install supervisor`): it finds `/etc/supervisor/supervisord.conf`; stopped (`sudo systemctl stop supervisor`), it shows `sudo systemctl enable --now supervisor` to copy; running, "won't let Visor in" with the group steps. Run them (Doctor never does), log in again: Doctor goes to "can watch, but not edit" with the `chgrp`/`chmod` command for `/etc/supervisor/conf.d`, and the dashboard says "Read-only: … belongs to root". Run it: everything is green and configs can be added.
- [ ] GNOME without the AppIndicator extension (Fedora before installing it): Doctor warns that there is no tray, with the `dnf` command.

