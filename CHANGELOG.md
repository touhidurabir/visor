# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Wails v3.0.0 app scaffold (Vue 3 + TypeScript frontend), bundle ID `dev.getvisor.visor`, minimum macOS 13.
- `internal/supervisor/xmlrpc`: dependency-free XML-RPC codec for supervisord. Log tails containing ANSI colour codes, other control characters, `\r` or invalid UTF-8 decode byte-for-byte instead of failing.
- `internal/supervisor`: typed client for every supervisord RPC Visor needs (status, start/stop/restart, groups, signals, log tails, reloadConfig/add/remove, restart/shutdown) over a pluggable `Transport` (unix socket and TCP), with basic auth, per-call timeouts, and faults matchable via `errors.Is` (`ErrNotRunning`, `ErrBadName`, …, plus `ErrUnreachable` and `ErrUnauthorized`).
- `internal/supervisor/supervisortest`: test harness that runs a real supervisord in a temp dir with dummy programs, and always shuts it down (killing stuck programs) even when a test fails or panics. Integration tests cover process control, groups, signals, log tails, `reloadConfig` add/remove, and daemon restart.
- `internal/config`: read-only parser for supervisord.conf (continuation lines, `;` inline comments only after whitespace, `%(here)s` and `%(name)02d` expansion) and config discovery that finds the socket or port, credentials, include globs and pidfile. The live check now finds the socket this way.
- `internal/app` `ServerService` lists processes to the frontend (first window shows name, group and state, or an error panel). `scripts/sandbox.sh up|down|status` runs a throwaway supervisord under `/tmp/visor-sandbox` with 12 sample processes, and `task dev` now always runs against it, so development never touches the real supervisord.
- GitHub Actions CI on macOS: `go vet`, golangci-lint, `go test -race` (with a real supervisord), a check that core `internal/` packages never import Wails (`scripts/check-core-imports.sh`), and frontend typecheck, tests and build. All actions are pinned by commit SHA.
- `internal/monitor`: one poll loop (default every 2s, `Poke` to poll now) that snapshots supervisord, diffs snapshots into events (state changed, added, removed, restarted between polls, unreachable once per outage, reconnected; the first contact after starting unreachable is not a reconnect) and computes aggregate health (error on FATAL, an unexpected EXITED status or an unreachable daemon; warning on STARTING, BACKOFF or STOPPING). Subscribers never block the loop: one that falls behind gets the latest snapshot with the events it missed merged in. Survives supervisord restarts.
