#!/usr/bin/env bash
set -euo pipefail
source .github/tool-versions.env
destination=${1:?usage: install-prometheus.sh destination-directory}
mkdir -p "$destination"
archive="prometheus-${PROMETHEUS_VERSION}.linux-amd64.tar.gz"
base="https://github.com/prometheus/prometheus/releases/download/v${PROMETHEUS_VERSION}"
curl --fail --location --retry 2 "$base/$archive" -o "$destination/$archive"
curl --fail --location --retry 2 "$base/sha256sums.txt" -o "$destination/sha256sums.txt"
(
    cd "$destination"
    awk -v archive="$archive" '$2 == archive || $2 == "*" archive' sha256sums.txt > selected-checksum.txt
    test -s selected-checksum.txt
    sha256sum --check selected-checksum.txt
    tar -xzf "$archive" --strip-components=1 --wildcards '*/prometheus' '*/promtool'
)
