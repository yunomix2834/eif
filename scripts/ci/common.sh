#!/usr/bin/env bash
set -euo pipefail

log_info() {
  printf '[eif-backend-ci] %s\n' "$*"
}

fail() {
  printf '[eif-backend-ci] ERROR: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

calculate_sha256() {
  sha256sum "$1" | awk '{print $1}'
}

write_github_output() {
  local key=$1
  local value=$2
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s=%s\n' "$key" "$value" >>"$GITHUB_OUTPUT"
  fi
}
