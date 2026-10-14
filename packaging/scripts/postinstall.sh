#!/bin/sh
# postinstall of the deb and rpm packages.
#
# The audit log directory is created here, not shipped: a package manager
# resets the owner of a directory it owns on every upgrade. At the first
# installation the user that Gitaly runs hooks as (GITLAB_REPOPOLICY_USER,
# default git) owns it and reads config.yaml through its group. Without that
# user, for example before GitLab is installed, the package only warns:
# "gitlab-repopolicy install-hook --user <user>" sets the same owners.
#
# An upgrade replaces an unchanged config.yaml with a root:root file; its
# group is set back to the group of the log directory. Owners set by an
# administrator are not changed.
set -e

config=/etc/gitlab-repopolicy/config.yaml
logdir=/var/log/gitlab-repopolicy

first=false
case "$1" in
	configure) [ -z "$2" ] && first=true ;; # deb: configure <old version>
	1) first=true ;;                        # rpm: number of installed versions
esac

if [ ! -d "$logdir" ]; then
	mkdir -p "$logdir"
	chmod 0750 "$logdir"
fi

if [ "$first" = true ]; then
	user="${GITLAB_REPOPOLICY_USER:-git}"
	if id "$user" > /dev/null 2>&1; then
		group="$(id -gn "$user")"
		chgrp "$group" "$config"
		chown "$user:$group" "$logdir"
	else
		echo "gitlab-repopolicy: user $user does not exist; after GitLab is installed run" >&2
		echo "  gitlab-repopolicy install-hook --hooks-dir <custom_hooks_dir> --user <user>" >&2
	fi
else
	group="$(stat -c %G "$logdir")"
	if [ "$group" != root ] && [ "$(stat -c %G "$config")" = root ]; then
		chgrp "$group" "$config"
	fi
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
