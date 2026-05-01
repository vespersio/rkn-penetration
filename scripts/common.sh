#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.tmp/go-build}"
export GOMODCACHE="${GOMODCACHE:-${ROOT_DIR}/.tmp/go-mod}"

log() {
  local scope="$1"
  shift
  printf '[%s] %s\n' "${scope}" "$*"
}

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "${cmd}" >&2
    exit 1
  fi
}

dattool() {
  (cd "${ROOT_DIR}" && go run ./tools/dattool "$@")
}

download_dat() {
  local scope="$1"
  local url="$2"
  local output="$3"

  log "${scope}" "downloading upstream $(basename "${output}")"
  curl -fsSL --retry 3 --retry-delay 2 --retry-connrefused "${url}" -o "${output}"
  if [[ ! -s "${output}" ]]; then
    printf '[%s] downloaded file is empty: %s\n' "${scope}" "${output}" >&2
    exit 1
  fi
}

write_sha256() {
  local file="$1"
  local sha_file="${file}.sha256"
  local dir
  local base
  dir="$(dirname "${file}")"
  base="$(basename "${file}")"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "${dir}" && sha256sum "${base}" > "${base}.sha256")
  else
    (cd "${dir}" && shasum -a 256 "${base}" > "${base}.sha256")
  fi

  if [[ ! -s "${sha_file}" ]]; then
    printf 'failed to write checksum: %s\n' "${sha_file}" >&2
    exit 1
  fi
}

check_sha256() {
  local sha_file="$1"
  local dir
  local base
  dir="$(dirname "${sha_file}")"
  base="$(basename "${sha_file}")"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "${dir}" && sha256sum -c "${base}")
  else
    (cd "${dir}" && shasum -a 256 -c "${base}")
  fi
}
