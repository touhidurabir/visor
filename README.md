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
wails3 dev     # run with hot reload
wails3 build   # build bin/visor
wails3 package # build bin/visor.app
go test ./...  # Go tests
```

## License

[MIT](LICENSE)
