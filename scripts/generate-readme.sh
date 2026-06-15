#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

github_slug_from_remote_url() {
  local remote_url="$1"
  if [[ "${remote_url}" =~ ^git@github\.com:([^/]+/[^/.]+)(\.git)?$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return
  fi
  if [[ "${remote_url}" =~ ^https://github\.com/([^/]+/[^/.]+)(\.git)?$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return
  fi
}

detect_upstream_repo_slug() {
  local upstream_ref remote remote_url
  upstream_ref="$(git -C "${repo_root}" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
  if [[ -z "${upstream_ref}" ]]; then
    return
  fi
  remote="${upstream_ref%%/*}"
  remote_url="$(git -C "${repo_root}" config --get "remote.${remote}.url" 2>/dev/null || true)"
  if [[ -z "${remote_url}" ]]; then
    return
  fi
  github_slug_from_remote_url "${remote_url}"
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

repo_slug="${README_REPO_SLUG:-}"
if [[ -z "${repo_slug}" ]]; then
  repo_slug="$(detect_upstream_repo_slug)"
fi
repo_slug="${repo_slug:-hydra-gitops/hydra}"

tap_repo_slug="${README_TAP_REPO_SLUG:-}"
if [[ -z "${tap_repo_slug}" ]]; then
  tap_repo_slug="$(extract_tap_repo_slug "${repo_root}/.github/secrets/repos/${repo_slug}/publish.yaml")"
fi
tap_repo_slug="${tap_repo_slug:-hydra-gitops/homebrew-tap}"

version="${1:-}"

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
