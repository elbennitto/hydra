#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly generator="${script_dir}/generate-prose-quality-report.py"

usage() {
  cat >&2 <<'EOF'
Usage: report-prose-docs.sh [path] [--output-dir DIR]

Runs Vale + readability on PATH, then writes a Sonar-style HTML report with
line hotspots, snippets, and per-file reading-level metrics.

Default path: docs/manual/tutorials/introduction/
Default output: .prose-quality-report/index.html

Examples:
  ./scripts/report-prose-docs.sh
  ./scripts/report-prose-docs.sh docs/manual/tutorials/introduction/
  ./scripts/report-prose-docs.sh docs/manual/ --output-dir /tmp/docs-report
EOF
}

target="docs/manual/tutorials/introduction/"
output_dir="${repo_root}/.prose-quality-report"

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --output-dir)
      output_dir="$2"
      shift 2
      ;;
    *)
      target="$1"
      shift
      ;;
  esac
done

if [[ ! -x "${repo_root}/scripts/lint-prose-docs.sh" ]]; then
  echo "Missing lint-prose-docs.sh" >&2
  exit 1
fi
if [[ ! -x "${repo_root}/scripts/report-readability-docs.sh" ]]; then
  echo "Missing report-readability-docs.sh" >&2
  exit 1
fi
if [[ ! -f "${generator}" ]]; then
  echo "Missing report generator: ${generator}" >&2
  exit 1
fi

cd "${repo_root}"
mkdir -p "${output_dir}"

vale_json="${output_dir}/vale.json"
read_json="${output_dir}/readability.json"
html_out="${output_dir}/index.html"

"${repo_root}/scripts/lint-prose-docs.sh" --sync >/dev/null 2>&1 || true
"${repo_root}/scripts/lint-prose-docs.sh" --json "${target}" > "${vale_json}" || true
"${repo_root}/scripts/report-readability-docs.sh" --json "${target}" > "${read_json}"

python3 "${generator}" \
  --vale-json "${vale_json}" \
  --readability-json "${read_json}" \
  --output "${html_out}" \
  --repo-root "${repo_root}" \
  --target-label "${target}"

echo "Report: ${html_out}"
echo "View in browser: ./scripts/serve-prose-quality-report.sh"
echo "  → http://127.0.0.1:8765/"
echo "Export PDF: ./scripts/export-prose-quality-pdf.sh [path] [output.pdf]"
