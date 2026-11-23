# Visor's MCP server

Visor can run an [MCP](https://modelcontextprotocol.io) server, so Claude Code, Codex, Cursor and other AI tools can read and manage supervisord through Visor, on this computer and on the servers you allow.

Ask your AI tool "what's failing in supervisor?", "show me horizon's last errors", "which worker uses the most memory?" or "restart the queue workers on staging". It calls Visor's tools, and Visor does the work with the same checks, backups and confirmations as its own buttons.

- [Turn it on](#turn-it-on)
- [Connect your AI tool](#connect-your-ai-tool)
- [Access per machine](#access-per-machine)
- [When Visor asks you](#when-visor-asks-you)
- [Tools](#tools)
- [What AI tools never get](#what-ai-tools-never-get)
- [Security and privacy](#security-and-privacy)
- [Troubleshooting](#troubleshooting)

## Turn it on

Open **MCP server** in Visor's sidebar and turn on **Enable MCP server**. It is off by default.

- **Port:** 4567 by default. Visor listens on `127.0.0.1` only, never on your network, at `http://127.0.0.1:<port>/mcp` (Streamable HTTP).
- **Access token:** every request must carry it as `Authorization: Bearer <token>`. It's kept in your Keychain (macOS) or keyring (Linux), never in a file. **Show**, **Copy** and **Regenerate…** are next to it.
- The badge beside the title says **Running**, **Off** or **Port in use** (another app has the port: choose another one).

## Connect your AI tool

The **Connecting** card has the command or config for each tool, with your token filled in. Run it once.

**Claude Code.** Click **Add to Claude Code** (shown when Visor finds the `claude` command). It shows the command and runs it when you confirm. Or run it yourself:

```sh
claude mcp add --scope user --transport http visor http://127.0.0.1:4567/mcp --header "Authorization: Bearer <token>"
```

**Codex.** Put the token in a variable, then add the server:

```sh
export VISOR_TOKEN=<token>
codex mcp add visor --url http://127.0.0.1:4567/mcp --bearer-token-env-var VISOR_TOKEN
```

Or add these lines to `~/.codex/config.toml`:

```toml
[mcp_servers.visor]
url = "http://127.0.0.1:4567/mcp"
http_headers = { Authorization = "Bearer <token>" }
```

**Cursor, VS Code and other tools.** Use the JSON:

```json
{
  "mcpServers": {
    "visor": {
      "url": "http://127.0.0.1:4567/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

**After a new token.** **Regenerate…** disconnects every AI tool that uses the old token.
- **Claude Code:** click **Add to Claude Code** again. It replaces the old entry. By hand, run `claude mcp remove --scope user visor`, then the add command.
- **Other tools:** copy the new command or config from the Connecting card.
- **Open sessions:** start a new session, or reconnect (in Claude Code, `/mcp`).

## Access per machine

The **Access** card lists this computer and every saved server. Each one has a level:

| Level | What AI tools may do there |
|---|---|
| **Off** | Nothing. The machine is invisible to AI tools: `list_servers` doesn't list it. |
| **Read** | The read tools: status, processes, CPU and memory, logs, issues, activity, configs, checks and Doctor. |
| **Control** | Read, plus start, stop, restart, signals and applying changes. |
| **Change configs** | Control, plus saving, creating, removing and restoring configs. |

- **This computer** starts at **Control**. **Servers** start at **Off**.
- **Ask in Visor before changes**, per machine, makes Visor ask you before each change (see below).
- **Production servers** (tagged production) always ask, and the switch can't be turned off for them.
- **Read only servers** go no higher than Read.
- A server whose tab isn't open opens as a tab when an AI tool needs it and its access allows. A server that needs a password or a host key from you opens only from Visor, the first time.

Every tool takes an optional `server` argument, a server's name or id from `list_servers`. Leaving it out means this computer.

## When Visor asks you

When a change needs your answer, Visor comes forward with the question, for example *"Claude Code wants to restart horizon on prod"*:

- **Deny**, **Allow for 10 minutes** or **Allow**.
- It appears in the main window, or in the menu-bar popover when the window is closed. A notification points to it, and Esc denies.
- No answer within a minute counts as Deny. The AI tool is told *"The person declined in Visor."* and shouldn't retry.
- **Allow for 10 minutes** covers that one AI tool, that machine and that kind of change. It isn't offered on production servers or for KILL, which always ask.

## Tools

26 tools, in three levels. The MCP page lists the same table.

### Read

| Tool | What it does |
|---|---|
| `list_servers` | This computer and the saved servers AI tools may use: name, id, tag, whether connected, the access level, and Read only. Servers set to Off aren't listed. |
| `get_status` | supervisord's state, version and uptime, process counts, health, the failing processes, the open issue count, configs changed on disk but not applied, and the machine's CPU cores and total memory. Start here. |
| `list_processes` | Every process: name, group, state, PID, uptime or time since it stopped, last exit status, config file and description. Filter by state, config or name. |
| `get_process` | One process in depth: state, last error, recent state history, its open issues and its program settings. |
| `get_resource_usage` | CPU (100 % is one core) and memory of each running process with the children it starts, now and over the last hour (peak and average), sampled every 10 seconds. A worker started through Docker shows the Docker client's figures, not the container's. |
| `read_logs` | The end of a process's stdout or stderr log, 100 lines by default and at most 500, paged with a cursor. `errors_only` keeps error lines and the stack traces under them. |
| `search_logs` | Lines matching text in a process's log, newest first, with line numbers. The last megabyte by default; `whole_file` only when Visor's Settings allow it. |
| `read_supervisord_log` | The end of supervisord's own log: why it stopped, failed to start, or gave up on a process. |
| `list_issues` | Open issues (can't start, exited unexpectedly, keeps restarting, not reachable) with counts and times; resolved ones too when asked. |
| `get_activity` | What changed and who did it: by Visor, outside Visor, or via an AI tool. |
| `list_configs` | Config files in the include folder with their programs, and whether each changed on disk, is watched, or isn't loaded. |
| `read_config` | A config file's text and its hash, which `save_config` needs. |
| `check_config` | Visor's plain-English explanation of a config file or draft, and its problems, checked on that machine. Nothing is written. |
| `get_pending_changes` | What applying would do, as `supervisorctl update` would. Nothing is applied. |
| `draft_program` | Fills one of Visor's templates (Laravel queue worker, Horizon, scheduler, Node app, script) into a `[program:x]` block and checks it. Nothing is written. |
| `list_backups` | Saved versions of a config file, newest first, with ids for `restore_config`. |
| `diagnose` | Doctor on this computer, or the server report on a server: what works, what doesn't, and the fix. Nothing is fixed. |

### Control

| Tool | What it does |
|---|---|
| `start` | Starts a process, a group (`group:*`) or every process of a config file. |
| `stop` | Stops a process, a group or a config file's processes. |
| `restart` | Restarts a process, a group or a config file's processes. |
| `signal_process` | Sends HUP, USR1, USR2, TERM, INT, QUIT or KILL to a process. KILL always asks. |
| `apply_changes` | Applies configs changed on disk, as `supervisorctl update` does, for one file or all. A changed program in a `[group:]` restarts the whole group. |

### Change configs

| Tool | What it does |
|---|---|
| `save_config` | Replaces a config's text when it still has the hash `read_config` gave. Visor checks it, keeps a backup, writes it and applies that file. Refused when the checks find errors or the file changed since it was read. |
| `create_config` | Writes a new config into the include folder, checks it and loads its programs. Refuses an existing file or a program name already in use. |
| `remove_config` | Stops a config's processes and moves the file to Visor's backups. |
| `restore_config` | Puts back a backup or a removed file, then checks and applies it. |

## What AI tools never get

- `supervisord.conf` itself. Only program configs in the include folder can change.
- **Stop all** and **Restart all**, restarting or shutting down supervisord, and clearing logs.
- Passwords, Visor's settings, and adding or editing servers.

## Security and privacy

- **Local only.** The server listens on `127.0.0.1`. Requests without the token are refused, and so is any request with an `Origin` header, which shuts out web pages, including browser-based AI tools.
- **The token** lives in your Keychain or keyring and is read only once you use MCP. **Regenerate…** cuts off every tool that has the old one.
- **Rate limits.** A runaway agent is held to one change per second per machine and 30 calls per 10 seconds. An allowed change waits out the gap rather than failing.
- **Environment values are hidden.** Values in `environment=` lines, commented ones too, show as «hidden» unless you turn on **Let AI read environment values** under Privacy. When an AI tool saves a file, the lines it left as it read them keep their real values. A hidden line it changed is refused.
- **What leaves your computer.** Logs and configs an AI tool reads go to your AI provider. Visor can't filter secrets out of logs. Set machines whose logs you don't want shared to **Off**.
- **A record.** **Recent calls** lists the last 100 calls since Visor started (client, tool, machine, result). Changes show in Activity as *via Claude Code* (or the tool's name) and don't send notifications.

## Troubleshooting

- **Port in use.** Another app has the port. Pick another one on the MCP page, then update the URL in your AI tool (run the command again).
- **401 or "unauthorized".** The token changed. See [After a new token](#connect-your-ai-tool).
- **The tool can't see a server.** Set its access above Off, and open its tab once in Visor if it needs a password or a host key.
- **"The person declined in Visor."** You chose Deny, or nobody answered within a minute.
- **A test build shares the token.** Packaged test builds (`scripts/sign-check.sh`) use the same Keychain entry as your installed Visor, so Regenerate there disconnects your tools too ([docs/scripts.md](scripts.md)).
