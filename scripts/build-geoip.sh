#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/common.sh"

require_cmd curl
require_cmd go

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

mkdir -p "${DIST_DIR}"

url="$(dattool config-value --file "${ROOT_DIR}/config/sources.yml" --key geoip_dat_url)"
upstream="${tmp_dir}/geoip.dat"

download_dat geoip "${url}" "${upstream}"
log geoip "extracting configured tags"
log geoip "appending custom CIDR"
log geoip "validating CIDR"
log geoip "building dist/geoip.dat"
dattool build-geoip \
  --config "${ROOT_DIR}/config/geoip.yml" \
  --upstream "${upstream}" \
  --output "${DIST_DIR}/geoip.dat"

write_sha256 "${DIST_DIR}/geoip.dat"
log geoip "validating output"
"${ROOT_DIR}/scripts/validate-geoip.sh"

