# Scripts and local runs

How to run Visor on your Mac or Linux desktop for each kind of testing,
without touching your real supervisord, its configs, or Visor's own state
(`~/Library/Application Support/Visor`, `~/.config/Visor` on Linux).
The most used come first. Automated tests and the manual checklist are in
[testing.md](testing.md).

## Which one do I need?

| I want to… | Run |
|---|---|
| Work on the UI or Go code with hot reload | `wails3 task dev` |
| Get a throwaway supervisord with sample processes | `scripts/sandbox.sh up` |
| Try what only the installed app can do (notifications, launch at login, one Visor at a time, Keychain prompts) | `scripts/sign-check.sh adhoc`, then `scripts/sign-check.sh open` |
| Run the browser UI tests | `wails3 task test:e2e` |
| Run every check CI runs | [Checks before a pull request](#5-checks-before-a-pull-request) |
| Build a `.dmg` like the release does | `wails3 package`, then `scripts/release-dmg.sh` |
| Redraw the app icon | `go run ./scripts/appicon` |
| Try the packaged app on Linux (the AppImage) | `scripts/linux-check.sh build`, then `scripts/linux-check.sh open` |
| Build the Linux packages like the release does | `wails3 task linux:package` |

## 1. `wails3 task dev`: everyday development

```sh
wails3 task dev     # or: task dev
```

It starts the sandbox supervisord (step 2) and runs Visor with hot reload
against it. It sets:

- `VISOR_CONFIG=/tmp/visor-sandbox/supervisord.conf`, so Visor uses the sandbox and never auto-detects your real config;
- `VISOR_STATE_DIR=/tmp/visor-sandbox/state`, so issues, history, settings and backups stay in the sandbox.

Stop it with Ctrl-C. The sandbox keeps running; remove it with
`scripts/sandbox.sh down`.

A dev build is not an app bundle, so these don't work in it: notifications
("Not available in this build"), launch at login (greyed out) and the
one-Visor check. Use step 3 for those.

**Never run bare `wails3 dev`.** It connects to your real supervisord, so the
buttons stop and restart your real processes, and it writes your real state
folder.

## 2. `scripts/sandbox.sh`: a throwaway supervisord

```sh
scripts/sandbox.sh up        # start it (does nothing if it's already running)
scripts/sandbox.sh status    # is it running, how many processes
scripts/sandbox.sh down      # stop it, stop every sample process, delete the folder
```

Everything lives in `/tmp/visor-sandbox`: `supervisord.conf`, the socket
`s.sock`, `conf.d/*.ini`, `logs/` and Visor's `state/`. `up` always starts
from a clean folder, so editing a sample `.ini` lasts only until the next `up`.

A second, separate sandbox: `VISOR_SANDBOX_DIR=/tmp/visor-notify scripts/sandbox.sh up`.
The name must look like `/tmp/visor-<name>`; the script refuses anything
else because `down` deletes the folder. The UI tests use `/tmp/visor-e2e`.

The 12 sample processes, chosen so every state shows up:

| Config | Program | What it does |
|---|---|---|
| `shop-api.ini` | `shop-queue` ×4, `shop-scheduler` | run forever |
| `mailer.ini` | `mailer-worker` ×2 | run forever |
| | `mailer-digest` | exits 1 at once, goes **FATAL** (a "can't start" issue) |
| `reports.ini` | `reports-export` | `autostart=false`: stays **STOPPED** |
| | `reports-sync` | command doesn't exist: **FATAL** |
| `notes.ini` | `notes-indexer` | exits 0 after 2 s and isn't restarted: **EXITED**, expected |
| `flaky.ini` | `flaky` | exits 2 every 20 s and restarts (an "exited unexpectedly" issue) |

To stop supervisord **without** deleting the sandbox (e.g. to see Doctor's
"supervisord isn't running"), use the dashboard's supervisord menu → Shut
down, or:

```sh
supervisorctl -c /tmp/visor-sandbox/supervisord.conf shutdown
```

## 3. `scripts/sign-check.sh`: the packaged app

```sh
scripts/sandbox.sh up             # the app runs against the sandbox
scripts/sign-check.sh adhoc       # build and ad-hoc sign /tmp/visor-signcheck/bin/visor.app
scripts/sign-check.sh open        # launch it
```

This is the closest you get to the app people install. It builds a real
`visor.app` (as `wails3 package` does) into `/tmp/visor-signcheck`, signs it,
and prints its signature and what Gatekeeper says. `bin/visor.app` isn't
touched.

`open` launches it with `VISOR_CONFIG` pointing at the sandbox and
`VISOR_STATE_DIR=/tmp/visor-signcheck/state`. Rebuild after each code change
(`adhoc` again); the state folder is kept between builds.

Use it for:

- **Notifications.** macOS asks once whether Visor may notify; change it later in System Settings → Notifications → Visor (set "Persistent" there to keep banners on screen).
- **Launch at login.** See the warning under [Cleaning up](#cleaning-up).
- **One Visor at a time.** `scripts/sign-check.sh open` again while it runs: no second menu-bar icon, the window comes forward.
- **Keychain prompts** when saving a password.

`scripts/sign-check.sh identity:"Visor Self-Signed"` signs with a certificate
from your login keychain instead of ad-hoc (the signing spike used this).

Starting it fresh (first-launch Doctor): quit Visor, `rm -rf /tmp/visor-signcheck/state`,
then `scripts/sign-check.sh open`.

## 4. `wails3 task test:e2e`: browser UI tests

```sh
wails3 task test:e2e                           # everything
wails3 task test:e2e -- tests/doctor.spec.ts   # one spec
wails3 task test:e2e -- --headed --debug       # watch it run
DEBUG_SERVER=1 wails3 task test:e2e            # also print Visor's own log
```

It builds the frontend and `bin/visor-e2e` (the server-mode build, which keeps
passwords in memory, never the Keychain), then runs Playwright in WebKit.
Playwright starts `e2e/scripts/serve.sh` itself: it brings up a sandbox in
`/tmp/visor-e2e`, runs Visor on port 8766, and removes both when the run ends.
You don't run `serve.sh` by hand. `tests/doctor.spec.ts` (ports 8771–8775)
and the supervisord-down cases in the dashboard, connection and tray specs
(8776–8778) start their own extra Visors with fresh state.

Visor's log is always in `e2e/visor-server.log`. More in [testing.md](testing.md#end-to-end-ui-tests).

## 5. Checks before a pull request

What CI runs, in the order that fails fastest:

```sh
go vet ./... && golangci-lint run
scripts/check-core-imports.sh          # core packages under internal/ must not import Wails
go test ./...                          # integration tests start their own temp supervisord
npm --prefix frontend run typecheck
npm --prefix frontend test
wails3 task test:e2e
```

`go test -short ./...` skips the integration tests when you want a quick run.

A read-only check against your real supervisord (it only calls methods that
read): `VISOR_LIVE=1 go test -run TestLive -v ./internal/supervisor/`
(`VISOR_LIVE_SOCKET=<path>` picks another socket).

## 6. `scripts/release-dmg.sh`: a `.dmg` like the release

```sh
wails3 package                                         # bin/visor.app
scripts/release-dmg.sh bin/visor.app 0.0.0 bin/dmg     # bin/dmg/Visor-0.0.0-universal.dmg + .sha256
```

The release workflow runs the same script on a universal build. Locally,
`wails3 package` builds for your Mac only, so the file says "universal" but
isn't; that's fine for trying the install steps.

Opening it and dragging Visor to Applications is a **real install**: that
copy uses your real supervisord and `~/Library/Application Support/Visor`.
Only do it when that's what you want to test.

## 7. `go run ./scripts/appicon`: the app icon

```sh
go run ./scripts/appicon                  # draws build/appicon.png, build/appicon.icon/Assets/lens.png and build/linux/icons/
wails3 task common:generate:icons         # turns them into build/darwin/icons.icns
```

Then rebuild the app to see it.

## 8. On Linux

`wails3 task dev`, `scripts/sandbox.sh` and `wails3 task test:e2e` work the
same. You need GTK 4 and WebKitGTK 6.0 to build (`sudo apt install
build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev` on Ubuntu
24.04) and supervisord (`pipx install supervisor` keeps it in your home; the
sandbox doesn't use the distro's).

```sh
scripts/linux-check.sh build     # the AppImage, into /tmp/visor-linuxcheck
scripts/linux-check.sh open      # run it against the sandbox, state in /tmp/visor-linuxcheck/state
wails3 task linux:package        # bin/visor-x86_64.AppImage, bin/visor.deb, bin/visor.rpm
```

`linux-check.sh` is the Linux `sign-check.sh`: the installed-only behaviour
(launch at login, one Visor at a time, a coloured tray icon) needs an
installed copy, and an AppImage counts as one. On Ubuntu 23.10+ the first
`open` stops before the window and prints the AppArmor profile to add (two
`sudo` commands); after that it opens. A dev build (`~/…/bin/visor`) needs
its own profile the same way; Visor prints it for that path.

Installing the `.deb` or `.rpm` (`sudo apt install ./bin/visor.deb`) is a
**real install**: that copy uses your real supervisord and `~/.config/Visor`.

## Environment variables

| Variable | What it does | Set by |
|---|---|---|
| `VISOR_CONFIG` | Use this supervisord config; overrides the one saved in Settings and disables Choose… | `task dev`, `sign-check.sh open`, e2e |
| `VISOR_STATE_DIR` | Keep Visor's state (issues, history, settings, backups) here instead of `~/Library/Application Support/Visor` | same |
| `VISOR_SANDBOX_DIR` | Which sandbox folder `sandbox.sh` uses (`/tmp/visor-<name>`) | e2e (`/tmp/visor-e2e`) |
| `VISOR_PLATFORM` | Server build only: word the UI as on `darwin` or `linux` | e2e (`linux`; `platform.spec.ts` starts a `darwin` one) |
| `VISOR_VERSION` | The version `wails3 task linux:package` puts in the `.deb` and `.rpm` (default `0.0.0`) | release workflow |
| `DEBUG_SERVER=1` | Show Visor's log during e2e runs | you |
| `VISOR_LIVE=1`, `VISOR_LIVE_SOCKET` | Run the read-only live test against a real supervisord | you |
| `SIGN_IDENTITY` | Signing identity for `wails3 task darwin:package` (`-` = ad-hoc, the default) | `sign-check.sh`, release workflow |

## Cleaning up

- **Sandboxes:** `scripts/sandbox.sh down` (add `VISOR_SANDBOX_DIR=…` for another one).
- **Packaged test build:** quit it, then `rm -rf /tmp/visor-signcheck`.
- **Launch at login:** turn it off in Visor before you delete a test build. A login item from a test build starts that build at login *without* `VISOR_CONFIG`, so it connects to your real supervisord. If one is left over: `rm ~/Library/LaunchAgents/dev.getvisor.visor.plist` (Linux: `rm ~/.config/autostart/dev.getvisor.visor.desktop`).
- **Linux test AppImage:** `rm -rf /tmp/visor-linuxcheck`. A profile you added for it stays in `/etc/apparmor.d/` until you remove it (`sudo apparmor_parser -R <file> && sudo rm <file>`).
- **A saved test password:** `security delete-generic-password -s dev.getvisor.visor -a <config path>`.
- **Old copies of the app** (macOS may take the notification icon from an older one with the same bundle ID): list them with
  `/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -dump | grep -i 'path:.*visor.*\.app'`,
  then rebuild or delete the stale ones.
