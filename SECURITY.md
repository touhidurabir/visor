# Security

## Reporting a problem

Please don't open a public issue for a security problem. Report it privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**. Include what you found, how to reproduce it, and which Visor version you used. You'll get an answer within a week.

## What Visor can reach

Visor runs as your user, with your permissions. It never asks for an administrator password and never runs `sudo`.

- **supervisord:** Visor connects to the socket or port named in your `supervisord.conf` and can start, stop and restart processes and read their logs, like `supervisorctl`.
- **Config files:** Visor reads `supervisord.conf` and writes only the program files in its `[include]` folder, after showing you the change. It never edits `supervisord.conf` itself.
- **Its own state:** issues, history, notifications, settings and backups of every config it changed or removed, in `~/Library/Application Support/Visor`.
- **Passwords:** a supervisord username and password you save go in the macOS Keychain (service `dev.getvisor.visor`), never in a file.
- **Launch at login:** a file in `~/Library/LaunchAgents` (`dev.getvisor.visor.plist`), only when you turn it on.

Visor doesn't send anything over the network except to supervisord, and it has no analytics.
