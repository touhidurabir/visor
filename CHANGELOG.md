# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [1.0.0] - 2026-11-27

The first release: a menu-bar and tray app for supervisord on macOS and Linux.

### Processes and logs
- A dashboard of every config and its processes: state, uptime, PID, and why a failing one fails, with the matching `supervisorctl` command in each menu.
- Start, stop, restart or signal a process, a group or a whole config file, from the window or the menu-bar popover.
- Live stdout and stderr logs: follow, pause, search, mark error levels and save. Rotation is handled.
- CPU and memory for each process, counting the children it starts, sampled every 10 seconds, with a chart of the last hour.

### Configs
- Add a program from a form, a template (Laravel queue worker, Horizon, scheduler, Node app, script), by hand, by copying a config, or by importing files.
- Every save is checked first, shown as a diff, backed up and applied on its own, as `supervisorctl update` would. One click restores a backup.
- *What this config does* explains every program and every line in plain English.
- Visor never edits `supervisord.conf` itself.

### Knowing when something breaks
- Issues and notifications for a fatal start, an unexpected exit or a crash loop. The tray icon shows the worst state at a glance.
- Activity records what changed and who changed it: Visor, someone outside Visor, or an AI tool.
- Doctor checks the install, the config, the socket and its permissions, and shows the fix. It never runs `sudo` for you.

### Remote servers
- Manage supervisord on Linux servers over SSH, each in its own tab, with your keys, agent and `~/.ssh/config`. Nothing is installed on the server.
- A report shows what the SSH user can and can't do. Use sudo is optional, per server. Production servers ask before anything disruptive, and Read only servers change nothing.

### AI tools through MCP
- An MCP server on `127.0.0.1`, behind a token, with 26 tools for status, logs, issues, CPU and memory, configs and control.
- Access per machine (Off, Read, Control or Change configs), and Visor can ask you before each change. See [docs/MCP.md](docs/MCP.md).

What Visor doesn't do yet: [docs/known-issues.md](docs/known-issues.md).

[1.0.0]: https://github.com/touhidurabir/visor/releases/tag/v1.0.0
