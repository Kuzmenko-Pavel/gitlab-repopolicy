#!/bin/sh
# preremove of the deb and rpm packages: on removal, not on upgrade, the
# timers stop. The hook wrapper stays: the administrator removes it with
# "gitlab-repopolicy uninstall-hook" (architecture §18.3).
set -e

case "$1" in
	remove | 0) ;; # deb: remove; rpm: no version left
	*) exit 0 ;;
esac

if [ -d /run/systemd/system ]; then
	systemctl disable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer > /dev/null 2>&1 || true
fi
