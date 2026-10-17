#!/bin/sh
set -eu
service=${DPKG_MAINTSCRIPT_PACKAGE:?}.service
case "${1:-}" in
    remove|deconfigure)
        if [ -d /run/systemd/system ]; then
            deb-systemd-invoke stop "$service"
        fi
        ;;
esac
