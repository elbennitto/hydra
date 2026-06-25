#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly config_path="${repo_root}/.readability.yml"
readonly readability_sh="${repo_root}/tools/readability/readability.sh"

usage() {
  cat >&2 <<'EOF'
Usage: report-readability-docs.sh [--check] [--json] [--verbose] [path]

  --check     Exit 1 when files exceed thresholds in .readability.yml.
  --json      JSON output (same as --format json).
  --verbose   Show all metrics per file.

Default path: docs/manual/tutorials/introduction/

Examples:
  ./scripts/report-readability-docs.sh
  ./scripts/report-readability-docs.sh docs/manual/tutorials/introduction/
  ./scripts/report-readability-docs.sh --json docs/manual/tutorials/introduction/01-00-create-a-hydra-app.md
  ./scripts/report-readability-docs.sh --check docs/manual/tutorials/introduction/
EOF
}

if [[ ! -f "${config_path}" ]]; then
  echo "Missing readability config: ${config_path}" >&2
  exit 1
fi

if [[ ! -x "${readability_sh}" ]]; then
  echo "Missing readability wrapper: ${readability_sh}" >&2
  exit 1
fi

cd "${repo_root}"

check=0
json=0
verbose=0
target="docs/manual/tutorials/introduction/"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --check)
      check=1
      shift
      ;;
    --json)
      json=1
      shift
      ;;
    --verbose | -v)
      verbose=1
      shift
      ;;
    *)
      target="$1"
      shift
      ;;
  esac
done

cmd=( "${readability_sh}" --config "${config_path}" )
if [[ "${json}" -eq 1 ]]; then
  cmd+=( --format json )
else
  cmd+=( --format markdown )
fi
if [[ "${verbose}" -eq 1 ]]; then
  cmd+=( --verbose )
fi
if [[ "${check}" -eq 1 ]]; then
  cmd+=( --check )
fi

cmd+=( "${target}" )

exec "${cmd[@]}"
