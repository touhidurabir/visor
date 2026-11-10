#!/bin/sh
# After installing Visor: load its AppArmor profile (the .deb ships one, for
# Ubuntu's user-namespace rule) and refresh the menus and icons.

if [ -f /etc/apparmor.d/dev.getvisor.visor ] && command -v apparmor_parser >/dev/null 2>&1; then
  apparmor_parser -r -T -W /etc/apparmor.d/dev.getvisor.visor || echo "visor: could not load the AppArmor profile; see /etc/apparmor.d/dev.getvisor.visor" >&2
fi

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi

exit 0
