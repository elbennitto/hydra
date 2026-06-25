#!/usr/bin/env bash
set -euo pipefail

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repo_root="$(cd "${script_dir}/.." && pwd)"
readonly target="${1:-docs/manual/tutorials/introduction/}"
readonly output="${2:-${HOME}/Documents/hydra-readability-report.pdf}"

chrome_bin=""
for candidate in google-chrome google-chrome-stable chromium chromium-browser; do
  if command -v "${candidate}" >/dev/null 2>&1; then
    chrome_bin="${candidate}"
    break
  fi
done

if [[ -z "${chrome_bin}" ]]; then
  echo "Chrome/Chromium not found. Open the HTML report and use Export to PDF." >&2
  exit 1
fi

cd "${repo_root}"
"${repo_root}/scripts/report-prose-docs.sh" "${target}" >/dev/null

port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"

server_pid=""
cleanup() {
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

python3 -m http.server "${port}" --bind 127.0.0.1 --directory "${repo_root}/.prose-quality-report" &
server_pid=$!

ready=0
for _ in $(seq 1 20); do
  if curl -fsS "http://127.0.0.1:${port}/" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.15
done
if [[ "${ready}" -ne 1 ]]; then
  echo "Failed to start local report server on port ${port}" >&2
  exit 1
fi

mkdir -p "$(dirname "${output}")"
"${chrome_bin}" \
  --headless=new \
  --disable-gpu \
  --no-pdf-header-footer \
  --virtual-time-budget=8000 \
  --run-all-compositor-stages-before-draw \
  --print-to-pdf="${output}" \
  "http://127.0.0.1:${port}/?pdf=1" \
  >/dev/null 2>&1

echo "Wrote ${output}"
