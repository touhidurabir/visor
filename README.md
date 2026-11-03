# Visor

A free, open-source macOS menu-bar app for managing [supervisord](http://supervisord.org/).
View processes, start/stop/restart them, tail logs, and edit program configs safely — without `supervisorctl`.

> **Status:** early development (pre-v0.1.0). The first release is on its way.

## Install

1. Download `Visor-<version>-universal.dmg` from [Releases](https://github.com/touhidurabir/visor/releases), open it, and drag **Visor** into **Applications**.
2. Open Visor from Applications. Visor isn't signed by Apple yet, so the first time macOS says it can't check it for malicious software:
   - **macOS 15 and later:** choose **Done**, then open **System Settings → Privacy & Security**, scroll to *"Visor" was blocked*, choose **Open Anyway** and confirm.
   - **macOS 13 and 14:** Control-click Visor in Applications, choose **Open**, then **Open** again.
   - Or, in Terminal: `xattr -dr com.apple.quarantine /Applications/Visor.app`
3. Optional: check the download against the `.sha256` file next to it in the release: `shasum -a 256 -c Visor-<version>-universal.dmg.sha256`.

Visor needs macOS 13 or later and supervisord 4.x (`brew install supervisor`). On first launch it checks your setup and says how to fix anything missing.

### Known limitations of unsigned builds

- macOS asks once whether Visor may show notifications. If you say no, or turn them off later in System Settings → Notifications, every notification is still kept in Visor: the bell in the sidebar and the menu-bar popover show what's unread.
- Keep Visor in Applications: run from the disk image or Downloads, macOS moves it to a temporary place, and launch at login won't turn on.

## Development

Requirements: macOS 13+, Go 1.26+ (1.27.3 recommended), Node 24 (see `.nvmrc`), and the Wails v3 CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
wails3 doctor
```

```sh
wails3 task dev # run with hot reload against a throwaway sandbox supervisord
wails3 build    # build bin/visor
wails3 package  # build bin/visor.app
go test ./...   # Go tests (integration tests start their own temp supervisord)
```

Tests, including the Playwright UI tests (`wails3 task test:e2e`) and the manual native checklist: [docs/testing.md](docs/testing.md).

Use `wails3 task dev` for development. It starts a sandbox supervisord under
`/tmp/visor-sandbox` (`scripts/sandbox.sh`) and points Visor at it. Bare
`wails3 dev` connects to your real supervisord, so the buttons stop and
restart your real processes, and it keeps its state in
`~/Library/Application Support/Visor`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security problem, see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
