#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

output_dir=${1:-dist/security/source}
mkdir -p "$output_dir"
require_command govulncheck

log_info "Running govulncheck"
set +e
govulncheck ./... 2>&1 | tee "$output_dir/govulncheck.txt"
status=${PIPESTATUS[0]}
set -e
exit "$status"
