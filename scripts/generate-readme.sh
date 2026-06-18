#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

usage() {
  cat <<'EOF'
Usage: ./scripts/generate-readme.sh [--upstream-repo <owner/name>] [version]

Options:
  --upstream-repo <owner/name>  Override upstream repository slug for rendering.
  -h, --help                    Show this help text.
EOF
}

detect_actions_repo_slug() {
  if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_REPOSITORY:-}" ]]; then
    printf '%s\n' "${GITHUB_REPOSITORY}"
  fi
}

extract_tap_repo_slug() {
  local publish_yaml="$1"
  if [[ ! -f "${publish_yaml}" ]]; then
    return
  fi

  awk '
    /^[[:space:]]*homebrew:[[:space:]]*$/ { in_homebrew=1; next }
    in_homebrew && /^[^[:space:]]/ { in_homebrew=0 }
    in_homebrew && /^[[:space:]]*tap_deploy_target_repo:[[:space:]]*/ {
      value=$0
      sub("^[[:space:]]*tap_deploy_target_repo:[[:space:]]*", "", value)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if ((value ~ /^".*"$/) || (value ~ /^\047.*\047$/)) {
        value=substr(value, 2, length(value)-2)
      }
      print value
      exit
    }
  ' "${publish_yaml}"
}

repo_slug_override=""
version=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --upstream-repo)
      if [[ $# -lt 2 ]]; then
        echo "Missing value for --upstream-repo" >&2
        usage >&2
        exit 1
      fi
      repo_slug_override="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      if [[ -n "${version}" ]]; then
        echo "Unexpected extra argument: $1" >&2
        usage >&2
        exit 1
      fi
      version="$1"
      shift
      ;;
  esac
done

repo_slug="${README_REPO_SLUG:-}"
if [[ -n "${repo_slug_override}" ]]; then
  repo_slug="${repo_slug_override}"
fi
if [[ -z "${repo_slug}" ]]; then
  repo_slug="$(detect_actions_repo_slug)"
fi
repo_slug="${repo_slug:-hydra-gitops/hydra}"

tap_repo_slug="${README_TAP_REPO_SLUG:-}"
if [[ -z "${tap_repo_slug}" ]]; then
  tap_repo_slug="$(extract_tap_repo_slug "${repo_root}/.github/secrets/repos/${repo_slug}/publish.yaml")"
fi
tap_repo_slug="${tap_repo_slug:-hydra-gitops/homebrew-tap}"

if [[ -z "${version}" ]]; then
  latest_tag="$(git -C "${repo_root}" describe --tags --abbrev=0 2>/dev/null || true)"
  if [[ -n "${latest_tag}" ]]; then
    version="${latest_tag#v}"
  fi
fi

echo "[prepare] generate-readme.sh starting (repo=${repo_slug}, tap=${tap_repo_slug}, version=${version:-latest})"

go run "${script_dir}/render-readme-gotpl.go" \
  -template "${repo_root}/README.md.gotpl" \
  -output "${repo_root}/README.md" \
  -repo "${repo_slug}" \
  -tap-repo "${tap_repo_slug}" \
  -version "${version}"

echo "[prepare] generate-readme.sh finished"
