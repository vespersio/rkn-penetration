#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/common.sh"

require_cmd go

if [[ ! -s "${DIST_DIR}/geoip.dat" ]]; then
  printf '[geoip] dist/geoip.dat is missing or empty\n' >&2
  exit 1
fi

if [[ ! -s "${DIST_DIR}/geoip.dat.sha256" ]]; then
  printf '[geoip] dist/geoip.dat.sha256 is missing or empty\n' >&2
  exit 1
fi

log geoip "checking checksum"
check_sha256 "${DIST_DIR}/geoip.dat.sha256"
log geoip "checking tags"
dattool validate-geoip --dat "${DIST_DIR}/geoip.dat" --tag proxy

