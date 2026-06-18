#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly tutorial_root="${repo_root}/docs/manual/tutorials"

hydra_bin="${HYDRA_BIN:-hydra}"

if [[ ! -d "${tutorial_root}" ]]; then
  echo "Tutorial directory not found: ${tutorial_root}" >&2
  exit 1
fi

if ! command -v "${hydra_bin}" >/dev/null 2>&1; then
  echo "Hydra binary not found: ${hydra_bin}" >&2
  echo "Set HYDRA_BIN to your hydra executable path." >&2
  exit 1
fi

mapfile -t tutorial_files < <(find "${tutorial_root}" -type f -name '*.cast.yaml' | sort)

if [[ "${#tutorial_files[@]}" -eq 0 ]]; then
  echo "No tutorial files found under ${tutorial_root}" >&2
  exit 1
fi

echo "Re-recording ${#tutorial_files[@]} tutorial(s) using ${hydra_bin}..."

cd "${repo_root}"

for tutorial_file in "${tutorial_files[@]}"; do
  tutorial_rel_path="${tutorial_file#${repo_root}/}"

  echo "[record] ${tutorial_rel_path}"
  "${hydra_bin}" record file "${tutorial_rel_path}" -v
done

echo "Done. Tutorial casts were refreshed under docs/manual/tutorials/."
