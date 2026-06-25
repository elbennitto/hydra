#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly config_path="${repo_root}/.vale.ini"
readonly vale_sh="${repo_root}/tools/vale/vale.sh"

usage() {
  cat >&2 <<'EOF'
Usage: lint-prose-docs.sh [--sync] [--json] [vale-args...] [paths...]

  --sync    Download/update Vale style packages (Google, write-good).
  --json    Emit machine-readable JSON (same as vale --output=JSON).

With no path arguments, lints all Hydra docs under docs/**/*.md.

Examples:
  ./scripts/lint-prose-docs.sh --sync
  ./scripts/lint-prose-docs.sh docs/manual/tutorials/introduction/
  ./scripts/lint-prose-docs.sh --json docs/manual/tutorials/introduction/README.md
EOF
}

if [[ ! -f "${config_path}" ]]; then
  echo "Missing Vale config: ${config_path}" >&2
  exit 1
fi

if [[ ! -x "${vale_sh}" ]]; then
  echo "Missing Vale wrapper: ${vale_sh}" >&2
  exit 1
fi

cd "${repo_root}"

if [[ ! -d "${repo_root}/.vale/styles/Google" ]]; then
  "${vale_sh}" sync
fi

sync=0
json=0
pass_args=()
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --sync)
      sync=1
      shift
      ;;
    --json)
      json=1
      shift
      ;;
    *)
      pass_args+=("$1")
      shift
      ;;
  esac
done

if [[ "${sync}" -eq 1 ]]; then
  exec "${vale_sh}" sync
fi

cmd=( "${vale_sh}" )
if [[ "${json}" -eq 1 ]]; then
  cmd+=( --output=JSON )
fi

if [[ "${#pass_args[@]}" -gt 0 ]]; then
  cmd+=( "${pass_args[@]}" )
else
  cmd+=( "docs/**/*.md" )
fi

exec "${cmd[@]}"
