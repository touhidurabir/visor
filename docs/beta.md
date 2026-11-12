# Visor beta: a note for testers

Thank you for trying Visor before its first release. This beta (`v0.1.0-beta.N`)
is for a small group of people who use supervisord every day, on a Mac or on
Linux. It is early: expect rough edges, and please tell us about them.

## Install

Download the file for your system from the release you were sent, and follow
[Install in the README](../README.md#install):

- **Mac:** `Visor-<version>-universal.dmg`. It isn't signed by Apple yet, so
  the first open needs **Open Anyway** (the README shows where).
- **Ubuntu, Debian:** `visor_<version>_amd64.deb`. **Fedora:** `visor-<version>-1.x86_64.rpm`.
  **Any other distro:** `Visor-<version>-x86_64.AppImage`.

On Ubuntu with the AppImage, Visor stops on its first run and prints two
`sudo` commands that let its window open (an AppArmor profile); run them once.
The `.deb` does this for you.

## What to try

Use Visor with your own supervisord, the way you use `supervisorctl` today:

1. **First launch.** Doctor checks your setup. Does it find your config? If
   something is wrong (supervisord not running, a socket only root can use, a
   config folder you can't write), are its steps clear, and do they work?
2. **The dashboard.** Start, stop and restart processes, singly and in bulk.
   Does what Visor shows match `supervisorctl status`?
3. **Logs.** Open a process's logs, follow them, search them.
4. **Configs.** Add a program (Add new: the form or the text), edit one, look
   at the diff and the backup, Restore it, Remove one and Undo it.
   **Import files:** bring in `.ini` or `.conf` files you already have (from a
   repo, or written by an assistant), with Choose files… or by dropping them
   on Visor's window. Copy a config into your include folder by hand, too:
   Visor should say it isn't loaded and offer **Load**.
5. **Issues and notifications.** Let something fail (a bad command, a program
   that exits). Do the banner and the Issues page say what happened?
6. **The menu bar or tray.** The popover (on GNOME: the tray menu's **Show
   status…**), its buttons, the unread count.
7. **Launch at login** (Visor then starts in the menu bar or tray only, with
   no window) and **Settings**, including a saved password.

Tested before this beta: macOS, and Ubuntu 24.04 with GNOME on Wayland and
on Xorg. Not yet tested by us: Fedora and the `.rpm`, and KDE Plasma. If you
use them, your report is especially welcome.

Visor never runs `sudo` and never edits your main `supervisord.conf`
(`[supervisord]` and the sockets); it writes only the program files in your
include folder, and backs each one up first.

## Tell us

Open an issue in the repository with **Bug report** or **Idea**
(Issues → New issue). For a bug, include:

- what you did, what you expected and what happened (a screenshot helps);
- your Visor version (About Visor on a Mac, the file name on Linux), your OS
  (macOS version, or distro, desktop and package) and `supervisord --version`;
- the config involved, with passwords taken out;
- Visor's log, if you can: quit Visor and start it from a terminal, then
  reproduce the problem. It logs there.
  - Mac: `/Applications/Visor.app/Contents/MacOS/visor`
  - Linux: `visor` (the `.deb` or `.rpm`), or `./Visor-<version>-x86_64.AppImage`

## Where Visor keeps things

| | Mac | Linux |
|---|---|---|
| Settings, issues, history, notifications | `~/Library/Application Support/Visor` | `~/.config/Visor` |
| Backups of configs it changed or removed | `…/Visor/backups` | `~/.config/Visor/backups` |
| A saved supervisord password | the login Keychain (`dev.getvisor.visor`) | your keyring (GNOME Keyring, KWallet) |
| Launch at login | `~/Library/LaunchAgents/dev.getvisor.visor.plist` | `~/.config/autostart/dev.getvisor.visor.desktop` |

To start over, quit Visor and remove the state folder. Uninstall with
Visor → Quit and dragging it to the Bin (Mac), `sudo apt remove visor` or
`sudo dnf remove visor`, or by deleting the AppImage.
