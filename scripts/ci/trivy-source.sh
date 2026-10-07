#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=common.sh
source "$script_dir/common.sh"

output_dir=${1:-dist/security/source}
mkdir -p "$output_dir"

require_command trivy

log_info "Creating source vulnerability/misconfiguration report"
trivy fs \
  --scanners vuln,misconfig \
  --format json \
  --output "$output_dir/trivy-fs.json" \
  .

log_info "Creating source SBOM"
trivy fs \
  --scanners vuln \
  --format cyclonedx \
  --output "$output_dir/sbom.cdx.json" \
  .

scan_status=0

log_info "Failing on CRITICAL source vulnerabilities/misconfigurations"
if ! trivy fs \
  --scanners vuln,misconfig \
  --severity CRITICAL \
  --exit-code 1 \
  .; then
  scan_status=1
fi

# Do not persist a JSON secret report: it can itself contain sensitive snippets.
log_info "Failing on any detected secret"
if ! trivy fs \
  --scanners secret \
  --exit-code 1 \
  --format table \
  .; then
  scan_status=1
fi

exit "$scan_status"
