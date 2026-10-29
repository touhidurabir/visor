# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Wails v3.0.0 app scaffold (Vue 3 + TypeScript frontend), bundle ID `dev.getvisor.visor`, minimum macOS 13.
- `internal/supervisor/xmlrpc`: dependency-free XML-RPC codec for supervisord. Log tails containing ANSI colour codes, other control characters, `\r` or invalid UTF-8 decode byte-for-byte instead of failing.
