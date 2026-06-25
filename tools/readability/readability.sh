#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly tool_dir="${script_dir}"
readonly readability_version="3.1.0"
readonly readability_asset="readability_linux_amd64.tar.gz"
readonly readability_url="https://github.com/adaptive-enforcement-lab/readability/releases/download/v${readability_version}/${readability_asset}"
readonly readability_bin="${tool_dir}/readability"

install_readability() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' RETURN

  echo "Installing readability ${readability_version} to ${tool_dir}..." >&2
  curl -fsSL "${readability_url}" -o "${tmp}/${readability_asset}"
  tar -xzf "${tmp}/${readability_asset}" -C "${tmp}"
  install -m 0755 "${tmp}/readability_linux_amd64" "${readability_bin}"
}

if [[ ! -x "${readability_bin}" ]]; then
  install_readability
fi

exec "${readability_bin}" "$@"
