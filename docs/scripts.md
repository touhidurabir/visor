# Scripts and local runs

How to run Visor on your Mac or Linux desktop for each kind of testing,
without touching your real supervisord, its configs, or Visor's own state
(`~/Library/Application Support/Visor`, `~/.config/Visor` on Linux).
The most used come first. Automated tests and the manual checklist are in
[testing.md](testing.md).

## How the pieces fit

| Layer | What it is | Used for |
|---|---|---|
| `wails3` | The Wails CLI. `wails3 build`, `wails3 package` and `wails3 dev` are shortcuts into the Taskfiles. | building, packaging, bindings |
| `wails3 task <name>` | Runs a task from `Taskfile.yml`, which pulls in `build/Taskfile.yml`, `build/darwin/Taskfile.yml` and `build/linux/Taskfile.yml`. `wails3 task --list` shows them. | anything in the Taskfiles |
| `npm --prefix frontend …`, `npm --prefix e2e …` | The Vue frontend and the Playwright suite | typecheck, unit tests, a frontend-only build |
| `scripts/*.sh` | Our own scripts. They wrap the above with a sandbox, a separate state folder and signing. | testing a real build without touching your setup |

The Task runner is built into `wails3`, so `wails3 task dev` works without
installing anything. A bare `task dev` needs [Task](https://taskfile.dev)
installed separately.

**Safe vs. real.** A Visor started without `VISOR_CONFIG` and `VISOR_STATE_DIR`
uses your **real** supervisord and your real state folder. These commands set
both for you:

- `wails3 task dev`
- `scripts/sign-check.sh open`
- `scripts/linux-check.sh open`
- `wails3 task test:e2e`

These don't, so they are **real** runs:

- `scripts/sign-check.sh open-real` (real supervisord, but its own state folder)
- bare `wails3 dev`
- `wails3 task run`
- starting `bin/visor` or `bin/visor.app` yourself
- installing a `.dmg`, `.deb` or `.rpm`

## Which one do I need?

| I want to… | Run |
|---|---|
| Work on the UI or Go code with hot reload | `wails3 task dev` |
| Get a throwaway supervisord with sample processes | `scripts/sandbox.sh up` |
| Try what only the installed app can do (notifications, launch at login, one Visor at a time, Keychain prompts) | `scripts/sign-check.sh adhoc`, then `scripts/sign-check.sh open` |
| Try the packaged app against my real supervisord, without touching my real Visor's state | `scripts/sign-check.sh adhoc`, then `scripts/sign-check.sh open-real` |
| Run the browser UI tests | `wails3 task test:e2e` |
| Run every check CI runs | [Checks before a pull request](#5-checks-before-a-pull-request) |
| Build a `.dmg` like the release does | `wails3 package`, then `scripts/release-dmg.sh` |
| Redraw the app icon | `go run ./scripts/appicon` |
| Try the packaged app on Linux (the AppImage) | `scripts/linux-check.sh build`, then `scripts/linux-check.sh open` |
| Build the Linux packages like the release does | `wails3 task linux:package` |
| Just build the binary or `.app` | `wails3 build` or `wails3 package` ([section 9](#9-plain-builds)) |
| Check a remote-server change against a real server | [Live checks](#10-live-checks-against-real-machines) |

## 1. `wails3 task dev`: everyday development

```sh
wails3 task dev     # or: task dev, if Task is installed
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

This is the closest you get to the app people install. `adhoc` builds a real
`visor.app` (as `wails3 package` does) into `/tmp/visor-signcheck`, signs it,
and prints its signature and what Gatekeeper says. `bin/visor.app` isn't
touched. Rebuild after each code change (run `adhoc` again).

Then open the build in one of two ways:

| | `open` | `open-real` |
|---|---|---|
| supervisord | the sandbox | your real one |
| Needs the sandbox up | yes | no |
| Start, stop, Save & apply | act on sample processes | act on **your real processes** |
| Visor's state folder | `/tmp/visor-signcheck/state` | `/tmp/visor-signcheck/state-real` |
| Your real Visor's state (`~/Library/Application Support/Visor`) | not touched | not touched |
| Keychain secrets (MCP token, passwords) and Launch at login | shared with your installed Visor | shared with your installed Visor |

### Against the sandbox: `open`

```sh
scripts/sandbox.sh up             # start the sample supervisord
scripts/sign-check.sh adhoc       # build and ad-hoc sign /tmp/visor-signcheck/bin/visor.app
scripts/sign-check.sh open        # launch it
```

`open` sets `VISOR_CONFIG` to the sandbox's `supervisord.conf` and
`VISOR_STATE_DIR` to `/tmp/visor-signcheck/state`. Nothing you do reaches your
real supervisord. This is the safe default for trying things out.

### Against your real supervisord: `open-real`

```sh
scripts/sign-check.sh adhoc       # build, as above
scripts/sign-check.sh open-real   # launch it against your real supervisord
```

Use this to see how the packaged app behaves with your own programs, without
starting the sandbox.

- **What it connects to.** `open-real` doesn't set `VISOR_CONFIG`, so Visor
  finds your config the way an installed copy does (for example
  `/opt/homebrew/etc/supervisord.conf`).
- **What is real.** Start, stop, restart, Save & apply and anything else you
  do act on your real supervisord and its processes.
- **What stays separate.** Visor's own state (settings, issues, history,
  saved servers, backups) goes to `/tmp/visor-signcheck/state-real`. Your real
  Visor's state isn't read or changed. Because that folder starts empty, the
  first launch looks like a first run: no saved servers, MCP off.
- **What is shared with your installed Visor.** Every test build shares
  these with it (`open` too), because they're keyed by the app, not by the
  state folder:
  - **Keychain secrets.** supervisord's password is keyed by the config
    path, so with `open-real` it's the same entry your everyday Visor uses.
    The MCP token is one entry for every build: **Regenerate** here
    disconnects the AI tools set up for your installed Visor.
  - **Launch at login.** Turning it on here replaces your installed Visor's
    login item with this build's.

Before you start:

- **Quit any other Visor first.** `open-real` refuses while one runs and
  shows its process ID. Every packaged Visor uses the same single-instance
  ID, so a second launch would only bring the running one forward, and you
  would be looking at the old build without noticing.
- **Turn launch at login off before deleting the build.** See
  [Cleaning up](#cleaning-up).

### What to test with it

- **Notifications.** macOS asks once whether Visor may notify; change it later in System Settings → Notifications → Visor (set "Persistent" there to keep banners on screen).
- **Launch at login.** See the warning under [Cleaning up](#cleaning-up).
- **One Visor at a time.** `scripts/sign-check.sh open` again while it runs: no second menu-bar icon, the window comes forward.
- **Keychain prompts** when saving a password.

### Starting fresh

The state folders are kept between builds. To see the first launch again
(onboarding, Doctor), quit Visor and delete the folder for the way you open it:

```sh
rm -rf /tmp/visor-signcheck/state        # then: scripts/sign-check.sh open
rm -rf /tmp/visor-signcheck/state-real   # then: scripts/sign-check.sh open-real
```

### Signing with a certificate

`scripts/sign-check.sh identity:"Visor Self-Signed"` signs with a certificate
from your login keychain instead of ad-hoc (the signing spike used this).
Open it with `open` or `open-real` as above.

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
You don't run `serve.sh` by hand. It also starts `bin/visor-e2e-ssh`, a test SSH
server on 127.0.0.1:8767 (user and password `e2e`) in front of the same sandbox,
for the server-tab specs (`servers.spec.ts`); the task builds it. `tests/doctor.spec.ts` (ports 8771–8775)
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

After changing an exported method of ServerService, ConfigService,
IssueService, LogService or InboxService, regenerate the per-tab wrappers the
frontend calls (`internal/app/api_gen.go`; a test fails until you do), then the
bindings:

```sh
go run ./scripts/genapi
wails3 generate bindings -clean=true -ts -i
```

Live checks against your real supervisord or a real server are in
[section 10](#10-live-checks-against-real-machines).

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

## 9. Plain builds

```sh
wails3 build                           # bin/visor
wails3 package                         # bin/visor.app, ad-hoc signed
wails3 task darwin:package:universal   # bin/visor.app for arm64 and amd64, as the release builds it
npm --prefix frontend run build        # frontend/dist only, without Go
```

`wails3 build` builds the frontend, generates the icons, then runs
`go build -tags production` into `bin/visor`. `wails3 package` runs the same
build and wraps the result in `bin/visor.app`. On Linux,
`wails3 task linux:package` does the packaging ([section 8](#8-on-linux)).

Both are **real** runs when you start them. Point them at the sandbox yourself:

```sh
VISOR_CONFIG=/tmp/visor-sandbox/supervisord.conf VISOR_STATE_DIR=/tmp/visor-sandbox/state bin/visor
```

For testing, `scripts/sign-check.sh` is usually what you want. It builds its
own `.app` and opens it against the sandbox.

**Don't use `wails3 task run`.** It builds `bin/visor.dev.app` and starts it
without the sandbox variables, so it connects to your real supervisord.

## 10. Live checks against real machines

Each one is skipped unless you set its variable. The two below only read.

```sh
VISOR_LIVE=1 go test -run TestLive -v ./internal/supervisor/                  # this Mac's supervisord
VISOR_SSH_TEST=visor-ubuntu go test -run TestLiveSSH -v ./internal/remote/    # a server, by its ~/.ssh/config alias
```

- `VISOR_LIVE_SOCKET=<path>` points the first test at another socket.
- `VISOR_SSH_SUDO=1` adds the sudo path to the second.
- `TestLiveServerTab` and `TestLiveActivity` in `./internal/app/` aren't read-only. They can restart processes, and `TestLiveActivity` also edits a config. Run them only on test machines.

The details are in [testing.md](testing.md#remote-servers).

## Environment variables

| Variable | What it does | Set by |
|---|---|---|
| `VISOR_CONFIG` | Use this supervisord config; overrides the one saved in Settings and disables Choose… | `task dev`, `sign-check.sh open`, e2e |
| `VISOR_STATE_DIR` | Keep Visor's state (issues, history, settings, backups) here instead of `~/Library/Application Support/Visor` | the same, plus `sign-check.sh open-real` |
| `VISOR_SANDBOX_DIR` | Which sandbox folder `sandbox.sh` uses (`/tmp/visor-<name>`) | e2e (`/tmp/visor-e2e`) |
| `VISOR_PLATFORM` | Server build only: word the UI as on `darwin` or `linux` | e2e (`linux`; `platform.spec.ts` starts a `darwin` one) |
| `VISOR_VERSION` | The version `wails3 task linux:package` puts in the `.deb` and `.rpm` (default `0.0.0`) | release workflow |
| `DEBUG_SERVER=1` | Show Visor's log during e2e runs | you |
| `VISOR_LIVE=1`, `VISOR_LIVE_SOCKET` | Run the read-only live test against a real supervisord | you |
| `VISOR_SSH_TEST`, `VISOR_SSH_SUDO`, `VISOR_SSH_SUDO_PASSWORD`, `VISOR_SSH_RESTART`, `VISOR_SSH_ACTIVITY` | Run the live remote-server tests against a real server (see [testing.md](testing.md#remote-servers)) | you |
| `SIGN_IDENTITY` | Signing identity for `wails3 task darwin:package` (`-` = ad-hoc, the default) | `sign-check.sh`, release workflow |

## Cleaning up

- **Sandboxes:** `scripts/sandbox.sh down` (add `VISOR_SANDBOX_DIR=…` for another one).
- **Packaged test build:** quit it, then `rm -rf /tmp/visor-signcheck` (this removes both state folders too).
- **Launch at login:** turn it off in Visor before you delete a test build. A login item from a test build starts that build at login *without* `VISOR_CONFIG`, so it connects to your real supervisord. If one is left over: `rm ~/Library/LaunchAgents/dev.getvisor.visor.plist` (Linux: `rm ~/.config/autostart/dev.getvisor.visor.desktop`).
- **Linux test AppImage:** `rm -rf /tmp/visor-linuxcheck`. A profile you added for it stays in `/etc/apparmor.d/` until you remove it (`sudo apparmor_parser -R <file> && sudo rm <file>`).
- **A saved test password:** `security delete-generic-password -s dev.getvisor.visor -a <config path>`.
- **Old copies of the app** (macOS may take the notification icon from an older one with the same bundle ID): list them with
  `/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -dump | grep -i 'path:.*visor.*\.app'`,
  then rebuild or delete the stale ones.
