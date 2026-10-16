#!/bin/sh
# preremove of the deb and rpm packages: on removal, not on upgrade, the
# timers stop and the hook wrapper goes from every custom_hooks_dir that
# install-hook recorded. Removing the package turns the checks off.
set -e

case "$1" in
	remove | 0) ;; # deb: remove; rpm: no version left
	*) exit 0 ;;
esac

if [ -d /run/systemd/system ]; then
	systemctl disable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer > /dev/null 2>&1 || true
fi

record=/var/lib/gitlab-repopolicy/hooks
if [ -f "$record" ]; then
	# uninstall-hook rewrites the record, so it is read first.
	dirs="$(cat "$record")"
	printf '%s\n' "$dirs" | while IFS= read -r dir; do
		[ -n "$dir" ] || continue
		if /usr/bin/gitlab-repopolicy uninstall-hook --hooks-dir "$dir" > /dev/null; then
			echo "gitlab-repopolicy: the hook is removed from $dir: pushes are no longer checked" >&2
		else
			echo "gitlab-repopolicy: remove $dir/pre-receive.d/50-gitlab-repopolicy by hand" >&2
		fi
	done
fi
