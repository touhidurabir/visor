# Visor

A free, open-source macOS menu-bar app for managing [supervisord](http://supervisord.org/).
View processes, start/stop/restart them, tail logs, and edit program configs safely — without `supervisorctl`.

> **Status:** early development (pre-v0.1.0). Nothing to install yet.

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

Use `wails3 task dev` for development. It starts a sandbox supervisord under
`/tmp/visor-sandbox` (`scripts/sandbox.sh`) and points Visor at it. Bare
`wails3 dev` connects to your real supervisord, so the buttons stop and
restart your real processes, and it keeps its state in
`~/Library/Application Support/Visor`.

## License

[MIT](LICENSE)
