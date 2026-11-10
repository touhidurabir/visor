# Visor

A free, open-source menu-bar and tray app for managing [supervisord](http://supervisord.org/), on macOS and Linux.
View processes, start/stop/restart them, tail logs, and edit program configs safely — without `supervisorctl`.

> **Status:** early development (pre-v0.1.0). The first release is on its way.

## Install

Visor needs supervisord 4.x. On first launch it checks your setup and says how to fix anything missing; it never runs `sudo` or installs anything itself.

### macOS

1. Download `Visor-<version>-universal.dmg` from [Releases](https://github.com/touhidurabir/visor/releases), open it, and drag **Visor** into **Applications**.
2. Open Visor from Applications. Visor isn't signed by Apple yet, so the first time macOS says it can't check it for malicious software:
   - **macOS 15 and later:** choose **Done**, then open **System Settings → Privacy & Security**, scroll to *"Visor" was blocked*, choose **Open Anyway** and confirm.
   - **macOS 13 and 14:** Control-click Visor in Applications, choose **Open**, then **Open** again.
   - Or, in Terminal: `xattr -dr com.apple.quarantine /Applications/Visor.app`
3. Optional: check the download against the `.sha256` file next to it in the release: `shasum -a 256 -c Visor-<version>-universal.dmg.sha256`.

Visor needs macOS 13 or later and supervisord (`brew install supervisor`).

### Linux

Visor is tested on Ubuntu 24.04+ and Fedora 40+, with GNOME and KDE Plasma, on x86_64. Other distros may work. Install supervisord from your distro (`sudo apt install supervisor` or `sudo dnf install supervisor`), then pick one:

- **Ubuntu, Debian:** `sudo apt install ./visor_<version>_amd64.deb`
- **Fedora:** `sudo dnf install ./visor-<version>-1.x86_64.rpm`
- **AppImage (any distro):** `chmod +x Visor-<version>-x86_64.AppImage`, then run it. Keep it somewhere that stays, such as `~/Applications`: from `/tmp`, launch at login stays off.

Check a download with `sha256sum -c <file>.sha256`.

Things to know:

- **The tray on GNOME** needs the AppIndicator extension. Ubuntu has it on; on Fedora, `sudo dnf install gnome-shell-extension-appindicator`, turn it on in Extensions and log in again. GNOME opens Visor's tray menu on a click: choose **Show status…** for the popover.
- **Ubuntu 23.10 and later** only let a program use the sandbox Visor's window needs when an AppArmor profile allows it. The `.deb` installs that profile. For the AppImage, Visor stops before its window opens and prints the two `sudo` commands that add one (they are also saved in `~/.config/Visor/apparmor-fix.txt`). Run them once and open Visor again.
- **supervisord from your distro runs as root**, and its socket is root's only (`chmod=0700`). Doctor shows how to let your user in through a group (`chmod=0770`, `chown=root:supervisor` in `supervisord.conf`, then join the group and log in again). Its `/etc/supervisor/conf.d` belongs to root too, so configs are read-only in Visor until the group can write there; Doctor shows that command as well.
- Saved passwords go to your keyring (GNOME Keyring or KWallet). Without one, Visor uses the password until you quit.

### Known limitations

- macOS asks once whether Visor may show notifications. If you say no, or turn them off later in System Settings → Notifications, every notification is still kept in Visor: the bell in the sidebar and the popover show what's unread.
- Keep Visor in Applications: run from the disk image or Downloads, macOS moves it to a temporary place, and launch at login won't turn on.
- On Wayland (Ubuntu's and Fedora's default), the desktop decides where the popover opens; it can't sit under the tray icon.
- Linux panels don't show text beside a tray icon, so the unread count is drawn on the icon (1–9, then 9+).

## Development

Requirements: Go 1.26+ (1.27.3 recommended), Node 24 (see `.nvmrc`), and the Wails v3 CLI. macOS 13+; or on Linux GTK 4 and WebKitGTK 6.0 (`sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev` on Ubuntu 24.04).

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
```

Which script or task to run for each kind of local testing (dev, the sandbox supervisord, the packaged app, UI tests, the `.dmg`, the AppImage): [docs/scripts.md](docs/scripts.md).

Tests, including the Playwright UI tests (`wails3 task test:e2e`) and the manual checklists for macOS and Linux: [docs/testing.md](docs/testing.md).

Use `wails3 task dev` for development. It starts a sandbox supervisord under
`/tmp/visor-sandbox` (`scripts/sandbox.sh`) and points Visor at it. Bare
`wails3 dev` connects to your real supervisord, so the buttons stop and
restart your real processes, and it keeps its state in
`~/Library/Application Support/Visor` (`~/.config/Visor` on Linux).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security problem, see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
