# Known issues

What Visor 1.0 doesn't do yet, or can't do, and what to do about it. Found something that isn't here? [Open an issue](https://github.com/touhidurabir/visor/issues).

## Activity and AI tools

- **A change you make by hand right after an AI tool's can be labelled as the tool's.** Visor marks a change as "via Claude Code" (or Codex, OpenCode, Cursor…) when it comes within about 15 seconds of that tool changing the same process or config. If you change the same one by hand in that time, Activity may show your change with the tool's name. It always names a tool you connected yourself.

## CPU and memory

- **macOS:** when Visor can't read a process (another user's, for example), the processes it started are left out too, even ones Visor could read. A process's CPU counts its live children only, not ones that have already ended.
- **Memory counts shared pages once per process**, as `top` does, so a program that forks workers (PHP-FPM, Gunicorn) shows more than it really uses.
- **A server mounted with `hidepid`** shows "–" for processes the SSH user isn't allowed to see.
- **Workers started through Docker** (`docker run` or `docker compose` in the program's command) show the Docker client's CPU and memory, not the container's. The container's processes belong to Docker, not to supervisord, so Visor can't see them. Use `docker stats` for those.

## Configs

- **A config restored with Use sudo comes back owned by root.** On a server that uses sudo, restoring a removed config writes it as root. supervisord reads it as before. To edit it later without sudo, give it back to your user with `sudo chown`.
- **A file added with Import's file picker gets the usual permissions (`0644`),** because the window can't see the original's. A file dragged onto the window, or a copy of an existing config, keeps the original's permissions.

## macOS

- macOS asks once whether Visor may show notifications. If you say no, or turn them off later in System Settings → Notifications, every notification is still kept in Visor: the bell in the sidebar and the popover show what's unread.
- Keep Visor in Applications. If you run it from the disk image or Downloads, macOS moves it to a temporary place, and Launch at login won't turn on.
- Visor isn't signed by Apple yet, so macOS asks you to allow it the first time. The steps are in the [README](../README.md#macos).

## Linux

- On Wayland (Ubuntu's and Fedora's default), the desktop decides where the popover opens, so it can't sit under the tray icon.
- Linux panels don't show text beside a tray icon, so the unread count is drawn on the icon (1–9, then 9+).
- The tray on GNOME needs the AppIndicator extension, and GNOME opens Visor's tray menu on a click: choose **Show status…** for the popover. See the [README](../README.md#linux).
- supervisord run by a user other than root or you (for example a `deploy` user) can't be managed in this computer's tab yet. Add it as a server instead: **Remote servers → Add a server**, host `localhost`, user `deploy`.
