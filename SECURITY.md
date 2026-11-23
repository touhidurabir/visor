# Security

## Reporting a problem

Please don't open a public issue for a security problem. Report it privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**. Include what you found, how to reproduce it, and which Visor version you used. You'll get an answer within a week.

## What Visor can reach

Visor runs as your user, with your permissions. On your computer it never asks for an administrator password and never runs `sudo`.

- **supervisord:** Visor connects to the socket or port named in your `supervisord.conf`. Like `supervisorctl`, it can start, stop and restart processes and read their logs.
- **Config files:** Visor reads `supervisord.conf` and writes only the program files in its `[include]` folder, after showing you the change and keeping a backup. It never edits `supervisord.conf` itself.
- **Its own state:** issues, history, activity, notifications, settings, saved servers and backups of every config it changed or removed. They're kept in `~/Library/Application Support/Visor` on macOS, or `~/.config/Visor` on Linux.
- **Secrets:** a supervisord password, server passwords, key passphrases, sudo passwords and the MCP token go in the macOS Keychain or your Linux keyring (service `dev.getvisor.visor`), never in a file. `servers.json` holds no secrets.
- **Launch at login:** only when you turn it on. On macOS that's `~/Library/LaunchAgents/dev.getvisor.visor.plist`; on Linux, `~/.config/autostart/dev.getvisor.visor.desktop`.

### Remote servers

- Visor connects only to the servers you add, over SSH, as the user you choose. It uses your keys, your SSH agent and `~/.ssh/config`.
- **Host keys:** a new server's key fingerprint is shown for you to check before you trust it. Visor keeps trusted keys in its own list, reads `~/.ssh/known_hosts` but never changes it, and refuses a key that has changed.
- **Nothing is installed on the server.** For the length of a connection, Visor runs a small Python helper there, through the SSH session, to read and write files.
- **sudo** runs on a server only when you turn on **Use sudo** for it. It's asked once per connection, and a refused password is never retried on its own, so it can't lock the account.
- Config backups stay on your computer. **Read only** servers change nothing, and **Production** servers ask before anything disruptive.

### The MCP server

- It's off until you turn it on. Then it listens on `127.0.0.1` only, never on your network.
- Every request needs the token. Requests with an `Origin` header (web pages) are refused, and calls are rate-limited.
- AI tools can do only what each machine's access level allows. Servers start at Off.
- Environment values in configs are hidden unless you allow them. Whatever an AI tool reads (logs, configs) goes to that tool's AI provider.
- Details: [docs/MCP.md](docs/MCP.md#security-and-privacy).

### Network

Visor talks to supervisord, to the servers you add (SSH), and to AI tools on this computer when MCP is on. It has no account, no analytics and no telemetry.
