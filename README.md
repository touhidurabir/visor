# Visor

A free, open-source menu-bar and tray app for [supervisord](http://supervisord.org/), on macOS and Linux.

See every process at a glance, start, stop and restart them, follow their logs, and edit program configs safely. Do it on your own computer and on your Linux servers over SSH, and let your AI coding tools help through MCP. It's the everyday `supervisorctl` work, in a window, with nothing to install next to supervisord.


- [Features](#features)
- [Install](#install)
- [Getting started](#getting-started)
- [Documentation](#documentation)
- [How it works](#how-it-works)
- [Development](#development)
- [Contributing](#contributing)

## Features

**Processes and logs**
- A dashboard of every config and its processes: state, uptime, PID, and why a failing one fails, with the matching `supervisorctl` command in each menu.
- Start, stop, restart or signal a process, a group or a whole config file.
- Live stdout and stderr: follow, pause, search, mark error levels, save. Rotation is handled for you.
- **CPU and memory** for each process, counting the children it starts. The figures update every 10 seconds, and each config page charts the last hour.

**Configs, edited safely**
- Add a program from a form, a template (Laravel queue worker, Horizon, scheduler, Node app, script), by hand, by copying a config, or by importing files.
- Every save is checked first: known keys, types, paths on disk, and unique names. You see the diff, a timestamped backup is kept, and only that file is applied, the way `supervisorctl update` would. One click restores a backup.
- *What this config does*: every program and every line explained in plain English, straight from the file. No AI.
- Visor never edits `supervisord.conf` itself, only the program files in its include folder.

**Knowing when something breaks**
- The menu-bar or tray icon shows when a process is starting, failing or stuck in a crash loop. The popover starts, stops and restarts processes in one click.
- **Issues** and notifications for a fatal start, an unexpected exit or a crash loop.
- **Activity** records what changed and who changed it: Visor, someone outside Visor (`supervisorctl`, a deploy script), or an AI tool.
- **Doctor** checks your setup when something's wrong (the install, the config, the socket and its permissions) and shows the fix. It never runs `sudo` for you.

**Remote servers**
- Manage supervisord on Linux servers over SSH, each in its own tab, with your own keys, agent and `~/.ssh/config`.
- Everything on the dashboard works there too, including config editing. Backups stay on your computer.
- Nothing is installed on the server. Visor runs a small Python helper for the length of the connection.
- A report shows what the SSH user can and can't do, with the fix. **Use sudo** is optional, per server.
- **Production** servers ask before anything disruptive. **Read only** servers change nothing.

**AI tools, through MCP**
- An MCP server on `127.0.0.1`, behind a token, with 26 tools: status, logs, issues, CPU and memory, configs, control and config changes.
- You choose the access per machine: Off, Read, Control or Change configs. Visor can ask you before each change.
- Details: [docs/MCP.md](docs/MCP.md).

## Install

Visor needs supervisord 4.x. On first launch it checks your setup and says how to fix anything missing. It never runs `sudo` or installs anything itself.

Downloads are on the [Releases](https://github.com/touhidurabir/visor/releases) page. Each file has a `.sha256` beside it.

### macOS

1. Download `Visor-<version>-universal.dmg` (Apple Silicon and Intel), open it, and drag **Visor** into **Applications**.
2. Open Visor from Applications. Visor isn't signed by Apple yet, so the first time macOS says it can't check it for malicious software:
   - **macOS 15 and later:** choose **Done**, then open **System Settings → Privacy & Security**, scroll to *"Visor" was blocked*, choose **Open Anyway** and confirm.
   - **macOS 13 and 14:** Control-click Visor in Applications, choose **Open**, then **Open** again.
   - Or, in Terminal: `xattr -dr com.apple.quarantine /Applications/Visor.app`
3. Optional: check the download with `shasum -a 256 -c Visor-<version>-universal.dmg.sha256`.

Visor needs macOS 13 or later and supervisord (`brew install supervisor`).

### Linux

Visor is tested on Ubuntu 24.04+ and Fedora 40+, with GNOME and KDE Plasma, on x86_64. Other distros may work. Install supervisord from your distro (`sudo apt install supervisor` or `sudo dnf install supervisor`), then pick one:

- **Ubuntu, Debian:** `sudo apt install ./visor_<version>_amd64.deb`
- **Fedora:** `sudo dnf install ./visor-<version>-1.x86_64.rpm`
- **AppImage (any distro):** `chmod +x Visor-<version>-x86_64.AppImage`, then run it. Keep it somewhere it can stay, such as `~/Applications`. If it runs from `/tmp`, Launch at login stays off.

Check a download with `sha256sum -c <file>.sha256`.

Things to know:

- **The tray on GNOME** needs the AppIndicator extension. Ubuntu has it on. On Fedora, run `sudo dnf install gnome-shell-extension-appindicator`, turn it on in Extensions and log in again. GNOME opens Visor's tray menu on a click: choose **Show status…** for the popover.
- **Ubuntu 23.10 and later** only let a program use the sandbox Visor's window needs when an AppArmor profile allows it. The `.deb` installs that profile. With the AppImage, Visor stops before its window opens and prints the two `sudo` commands that add one. They're also saved in `~/.config/Visor/apparmor-fix.txt`. Run them once and open Visor again.
- **supervisord from your distro runs as root**, and only root can use its socket (`chmod=0700`).
  - Doctor shows how to let your user in through a group: set `chmod=0770` and `chown=root:supervisor` in `supervisord.conf`, then join the group and log in again.
  - Root owns `/etc/supervisor/conf.d` too, so configs are read-only in Visor until the group can write there. Doctor shows that command as well.
- Saved passwords go to your keyring (GNOME Keyring or KWallet). Without a keyring, Visor keeps the password only until you quit.

### Known issues

A few things don't work everywhere yet: notifications macOS turned off, the popover's place on Wayland, CPU and memory for Docker workers, and more. See [docs/known-issues.md](docs/known-issues.md).

## Getting started

1. **Open Visor.** It finds your `supervisord.conf` in the usual places: `/opt/homebrew/etc`, `/usr/local/etc`, `/etc` and `/etc/supervisor`. If something's missing, Doctor says what and how to fix it. You can also choose the config in Settings.
2. **Use the dashboard.** Click a config to see its processes, logs, explanation and editor. **Add new** (⌘N, or Ctrl+N on Linux) creates a program.
3. **Add a server** (optional). Open **Remote servers** in the sidebar and choose **Add a server**. **Test** shows what that SSH user can do before you connect.
4. **Connect an AI tool** (optional). Open **MCP server**, turn it on, and click **Add to Claude Code** or copy the command for your tool. See [docs/MCP.md](docs/MCP.md).

## Documentation

| Read | For |
|---|---|
| [docs/MCP.md](docs/MCP.md) | The MCP server: connecting AI tools, access per machine, every tool, security |
| [docs/known-issues.md](docs/known-issues.md) | What Visor doesn't do yet, and what to do about it |
| [CHANGELOG.md](CHANGELOG.md) | What changed in each version |
| [SECURITY.md](SECURITY.md) | What Visor can reach, and how to report a security problem |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Setting up, the code layout and pull requests |
| [docs/scripts.md](docs/scripts.md) | Which script or task to run for local testing (dev, sandbox, packaged builds) |
| [docs/testing.md](docs/testing.md) | Automated tests, UI tests and the manual checklists |

## How it works

- **Visor talks to supervisord directly** over its XML-RPC socket or port, the same interface `supervisorctl` uses. It never runs `supervisorctl` or parses its output.
- **On servers** it connects over SSH (Go's `x/crypto/ssh`), forwards supervisord's socket, and runs a small Python helper (Python comes with supervisord) for file access and sudo. Nothing is installed, and the helper ends with the connection.
- **Configs are written safely.** Visor checks the file, makes a backup, writes it, and applies only that file: reload, then add, change or remove its groups.
- **Your data stays with you.** There's no account and no telemetry. Passwords and the MCP token live in the Keychain or keyring. Visor's state lives in `~/Library/Application Support/Visor` (macOS) or `~/.config/Visor` (Linux).
- Built with Go and [Wails v3](https://v3.wails.io/), with a Vue 3 + TypeScript UI in the system WebView.

## Development

Requirements: Go 1.26+ (1.27.4 recommended), Node 24 (see `.nvmrc`), and the Wails v3 CLI. On macOS, version 13 or later. On Linux, GTK 4 and WebKitGTK 6.0; on Ubuntu 24.04, run `sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev`.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
wails3 doctor
```

```sh
wails3 task dev            # run with hot reload against a throwaway sandbox supervisord
wails3 build               # build bin/visor
wails3 package             # macOS: build bin/visor.app
wails3 task linux:package  # Linux: build the AppImage, .deb and .rpm in bin/
go test ./...              # Go tests (integration tests start their own temp supervisord)
wails3 task test:e2e       # UI tests (Playwright) against a sandbox supervisord
```

Use `wails3 task dev` for development. It starts a sandbox supervisord under `/tmp/visor-sandbox` and points Visor at it. Bare `wails3 dev` connects to your real supervisord, so its buttons stop and restart your real processes.

Which script to run for each kind of local testing: [docs/scripts.md](docs/scripts.md). Tests and the manual checklists: [docs/testing.md](docs/testing.md).

## Contributing

Bug reports, ideas and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security problem privately, see [SECURITY.md](SECURITY.md).

## License

Copyright © 2026 Touhidur Rahman.

Visor is free software under the [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0), version 3 or any later version.

- **You can** use it, change it, share it, and even sell it.
- **If you distribute a changed Visor, or offer it to others as a network service,** you must share your changes under the same license, with the source, to the people who receive or use it.
- Using Visor, or changing it for your own use, needs nothing more.
