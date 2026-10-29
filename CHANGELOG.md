# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Wails v3.0.0 app scaffold (Vue 3 + TypeScript frontend), bundle ID `dev.getvisor.visor`, minimum macOS 13.
- `internal/supervisor/xmlrpc`: dependency-free XML-RPC codec for supervisord. Log tails containing ANSI colour codes, other control characters, `\r` or invalid UTF-8 decode byte-for-byte instead of failing.
- `internal/supervisor`: typed client for every supervisord RPC Visor needs (status, start/stop/restart, groups, signals, log tails, reloadConfig/add/remove, restart/shutdown) over a pluggable `Transport` (unix socket and TCP), with basic auth, per-call timeouts, and faults matchable via `errors.Is` (`ErrNotRunning`, `ErrBadName`, …, plus `ErrUnreachable` and `ErrUnauthorized`).
