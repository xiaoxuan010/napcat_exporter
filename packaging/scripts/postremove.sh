#!/bin/sh
set -eu
service=${DPKG_MAINTSCRIPT_PACKAGE:?}.service
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi
if [ "${1:-}" = purge ]; then
    deb-systemd-helper purge "$service"
fi
# Keep the system account to avoid reusing its UID for another service.
