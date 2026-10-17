#!/bin/sh
set -eu
package=${DPKG_MAINTSCRIPT_PACKAGE:?}
if ! getent group "$package" >/dev/null; then
    addgroup --system "$package"
fi
if ! getent passwd "$package" >/dev/null; then
    adduser --system --ingroup "$package" --no-create-home --home /nonexistent --disabled-login "$package"
fi
