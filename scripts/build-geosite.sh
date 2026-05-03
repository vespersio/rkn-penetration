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

url="$(dattool config-value --file "${ROOT_DIR}/config/sources.yml" --key geosite_dat_url)"
upstream="${tmp_dir}/geosite.dat"

download_dat geosite "${url}" "${upstream}"
log geosite "extracting configured categories"
log geosite "appending custom rules"
log geosite "applying sanitize keywords"
log geosite "validating rules"
log geosite "building dist/geosite.dat"
dattool build-geosite \
  --config "${ROOT_DIR}/config/geosite.yml" \
  --upstream "${upstream}" \
  --output "${DIST_DIR}/geosite.dat"

write_sha256 "${DIST_DIR}/geosite.dat"
log geosite "validating output"
"${ROOT_DIR}/scripts/validate-geosite.sh"
