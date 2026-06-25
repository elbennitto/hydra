#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly report_dir="${repo_root}/.prose-quality-report"
readonly port="${PORT:-8765}"

usage() {
  cat >&2 <<EOF
Usage: serve-prose-quality-report.sh [--generate] [path]

Serve the prose quality HTML report at http://127.0.0.1:${port}/

  --generate   Run report-prose-docs.sh before serving (optional path argument)

Examples:
  ./scripts/serve-prose-quality-report.sh
  ./scripts/serve-prose-quality-report.sh --generate docs/manual/tutorials/introduction/
EOF
}

generate=0
target="docs/manual/tutorials/introduction/"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --generate)
      generate=1
      shift
      ;;
    *)
      target="$1"
      shift
      ;;
  esac
done

if [[ "${generate}" -eq 1 ]]; then
  "${repo_root}/scripts/report-prose-docs.sh" "${target}"
fi

if [[ ! -f "${report_dir}/index.html" ]]; then
  echo "Missing ${report_dir}/index.html — run: ./scripts/report-prose-docs.sh" >&2
  exit 1
fi

url="http://127.0.0.1:${port}/"
echo "Serving prose quality report at ${url}"
echo "Press Ctrl+C to stop."
echo ""
echo "Tip: open that URL in Chrome/Firefox — do not open index.html in the editor (shows raw HTML)."

cd "${report_dir}"
exec python3 -m http.server "${port}" --bind 127.0.0.1
