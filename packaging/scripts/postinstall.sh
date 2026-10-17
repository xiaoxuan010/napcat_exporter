#!/bin/sh
set -eu
package=${DPKG_MAINTSCRIPT_PACKAGE:?}
service=$package.service
if [ "${1:-}" = configure ] && [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    # Never enable on install or upgrade. Respect policy-rc.d for restarts.
    if [ -n "${2:-}" ] && systemctl --quiet is-active "$service"; then
        deb-systemd-invoke try-restart "$service"
    fi
fi
