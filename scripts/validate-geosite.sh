#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/common.sh"

require_cmd go

if [[ ! -s "${DIST_DIR}/geosite.dat" ]]; then
  printf '[geosite] dist/geosite.dat is missing or empty\n' >&2
  exit 1
fi

if [[ ! -s "${DIST_DIR}/geosite.dat.sha256" ]]; then
  printf '[geosite] dist/geosite.dat.sha256 is missing or empty\n' >&2
  exit 1
fi

log geosite "checking checksum"
check_sha256 "${DIST_DIR}/geosite.dat.sha256"
log geosite "checking categories"
dattool validate-geosite --dat "${DIST_DIR}/geosite.dat" --tag proxy

