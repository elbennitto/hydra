#!/usr/bin/env bash
# Start MkDocs dev server with live reload for the Hydra user manual.
set -euo pipefail

readonly site_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${site_dir}"

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required" >&2
  exit 1
fi

npm ci --no-fund --no-audit

if [[ ! -d .venv ]]; then
  python3 -m venv .venv
fi

# shellcheck source=/dev/null
source .venv/bin/activate
pip install -q --upgrade pip
pip install -q -r requirements.txt
pip install -q -e .

echo "Starting MkDocs dev server (Ctrl+C to stop)..."
echo "Watches: manual/ (including .cast files) and site assets (see mkdocs.yml watch:)"
exec mkdocs serve -a 127.0.0.1:8000 "$@"
