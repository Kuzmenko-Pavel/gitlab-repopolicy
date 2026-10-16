#!/bin/sh
# postremove of the deb and rpm packages: a purge (deb) also removes the
# config, the token, break-glass grants and the state. The audit log stays:
# it is the record of what the hook decided.
set -e

case "$1" in
	purge) ;;
	*) exit 0 ;;
esac

rm -rf /etc/gitlab-repopolicy /var/lib/gitlab-repopolicy
