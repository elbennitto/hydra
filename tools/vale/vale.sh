#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/../.." && pwd)"
readonly tool_dir="${repo_root}/tools/vale"
readonly vale_version="3.15.1"
readonly vale_asset="vale_${vale_version}_Linux_64-bit.tar.gz"
readonly vale_url="https://github.com/vale-cli/vale/releases/download/v${vale_version}/${vale_asset}"
readonly vale_bin="${tool_dir}/vale"

install_vale() {
  mkdir -p "${tool_dir}"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' RETURN

  echo "Installing Vale ${vale_version} to ${tool_dir}..." >&2
  curl -fsSL "${vale_url}" -o "${tmp}/${vale_asset}"
  tar -xzf "${tmp}/${vale_asset}" -C "${tmp}"
  install -m 0755 "${tmp}/vale" "${vale_bin}"
}

if [[ ! -x "${vale_bin}" ]]; then
  install_vale
fi

exec "${vale_bin}" "$@"
