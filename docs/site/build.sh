#!/usr/bin/env bash
# Build the Hydra user manual static site (output: hydra/docs/site/site/).
set -euo pipefail

readonly site_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${site_dir}"

site_url_override=""
mkdocs_args=()

while (($# > 0)); do
  case "$1" in
    --site-url)
      if (($# < 2)); then
        echo "--site-url requires a value" >&2
        exit 1
      fi
      site_url_override="$2"
      shift 2
      ;;
    *)
      mkdocs_args+=("$1")
      shift
      ;;
  esac
done

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required" >&2
  exit 1
fi

npm ci --no-fund --no-audit

python3 -m venv .venv
# shellcheck source=/dev/null
source .venv/bin/activate
pip install -q --upgrade pip
pip install -q -r requirements.txt
pip install -q -e .

if [[ -n "${site_url_override}" ]]; then
  tmp_config="$(mktemp "${site_dir}/.mkdocs.XXXXXX.yml")"
  trap 'rm -f "${tmp_config}"' EXIT

  python3 - "${site_dir}/mkdocs.yml" "${tmp_config}" "${site_url_override}" <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1])
target = Path(sys.argv[2])
site_url = sys.argv[3]

lines = source.read_text(encoding="utf-8").splitlines()
for index, line in enumerate(lines):
    if line.startswith("site_url:"):
        lines[index] = f"site_url: {site_url}"
        break
else:
    raise SystemExit("mkdocs.yml is missing a site_url entry")

target.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY

  mkdocs build -f "${tmp_config}" "${mkdocs_args[@]}"
else
  mkdocs build "${mkdocs_args[@]}"
fi

echo "Built site: ${site_dir}/site/"
echo "See README.md for local preview commands."
