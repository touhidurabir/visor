# Contributing to Visor

Thanks for helping. Bug reports, ideas and pull requests are all welcome.

## Before you start

- **Bugs:** open an issue with the bug report form: what you did, what you expected, what happened, your macOS and supervisord versions (`supervisord --version`), and Visor's version (Visor → About Visor).
- **Bigger changes:** open an issue first so we can agree on the approach before you spend time on it.

## Setting up

You need macOS 13 or later, or Linux with GTK 4 and WebKitGTK 6.0 (see the [README](README.md#development)). You also need Go 1.27.3 (the official installer from go.dev), Node (see `.nvmrc`), supervisord (`brew install supervisor`, or your distro's package) and the Wails v3 CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
wails3 task dev   # runs Visor against a throwaway sandbox supervisord in /tmp/visor-sandbox
```

Always develop with `wails3 task dev`. Bare `wails3 dev` connects to your real supervisord, so the buttons stop and restart your real processes.

The other scripts (the sandbox, the packaged app for notifications and launch at login, the `.dmg`) and when to use each: [docs/scripts.md](docs/scripts.md).

## How the code is laid out

- `internal/supervisor`: XML-RPC client for supervisord (Visor never shells out to `supervisorctl`).
- `internal/config`, `internal/validate`, `internal/describe`, `internal/apply`: reading, checking, explaining and safely writing program configs.
- `internal/monitor`, `internal/issues`, `internal/notify`, `internal/logs`: watching processes, tracking failures, notifications and log tails.
- `internal/activity`: who changed what (Visor, outside Visor, an AI tool).
- `internal/doctor`, `internal/platform`, `internal/trayicon`: the setup checks, the macOS and Linux helpers, and the tray icon.
- `internal/remote`: servers over SSH: the tunnel to supervisord's socket, host keys, the capability report, and the Python helper (`bridge.py`) for files, sudo and process figures.
- `internal/usage`: CPU and memory per process tree, and an hour of history.
- `internal/mcp`: the MCP server's tools and access rules ([docs/MCP.md](docs/MCP.md)).
- `internal/settings`: settings, saved servers and MCP access.
- `internal/app`: the only package that talks to Wails; it exposes the others to the UI.
- `frontend/`: the Vue 3 + TypeScript UI.

Packages under `internal/` other than `internal/app` must not import Wails (`scripts/check-core-imports.sh` checks this).

## Checks to run before a pull request

```sh
go test ./...                         # integration tests start their own temp supervisord
go vet ./... && golangci-lint run
scripts/check-core-imports.sh
npm --prefix frontend run typecheck
npm --prefix frontend test
wails3 task test:e2e                  # Playwright UI tests against a sandbox supervisord
```

Every change under `internal/` comes with tests. UI changes come with an end-to-end test where one fits, and things only a real Mac can show go on the manual checklist in [docs/testing.md](docs/testing.md).

## Pull requests

- Keep each pull request to one change, and describe what it does, why, and how you checked it.
- Add a line to `CHANGELOG.md` under `## [Unreleased]`.
- Never point tests or dev runs at a real supervisord or its configs.

By contributing you agree that your work is released under the [MIT licence](LICENSE).
