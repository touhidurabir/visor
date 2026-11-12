#!/bin/sh
# Before removing Visor: unload its AppArmor profile (the package removes
# the file). On an upgrade the new package loads it again.

if [ -f /etc/apparmor.d/dev.getvisor.visor ] && command -v apparmor_parser >/dev/null 2>&1; then
  apparmor_parser -R /etc/apparmor.d/dev.getvisor.visor >/dev/null 2>&1 || true
fi

exit 0
