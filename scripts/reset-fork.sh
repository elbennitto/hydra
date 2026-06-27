#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/reset-fork.sh

Example:
  scripts/reset-fork.sh
EOF
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
secrets_root="${repo_root}/.github/secrets"

fork_path=""
fork_owner=""
fork_repo=""
fork_url=""
package_url=""
package_ci_url=""
upstream_path=""
upstream_url=""

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -ne 0 ]]; then
  echo "This script does not accept positional parameters." >&2
  usage >&2
  exit 1
fi

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "${cmd} is required" >&2
    exit 1
  fi
}

prompt_with_default() {
  local label="$1"
  local default_value="$2"
  local reply=""

  while true; do
    if [[ -n "${default_value}" ]]; then
      read -r -p "${label} [${default_value}]: " reply
      if [[ -z "${reply}" ]]; then
        reply="${default_value}"
      fi
    else
      read -r -p "${label}: " reply
    fi

    if [[ -n "${reply}" ]]; then
      printf '%s\n' "${reply}"
      return
    fi

    echo "Value must not be empty." >&2
  done
}

detect_default_fork_path() {
  local -a paths

  if [[ -d "${secrets_root}/repos" ]]; then
    mapfile -t paths < <(
      find "${secrets_root}/repos" -mindepth 2 -maxdepth 2 -type d 2>/dev/null \
        | sed -e "s#^${secrets_root}/repos/##" \
        | sort
    )

    if [[ ${#paths[@]} -eq 1 ]]; then
      printf '%s\n' "${paths[0]}"
      return
    fi

    if [[ ${#paths[@]} -gt 1 ]]; then
      echo "Found existing fork secret directories:" >&2
      printf '  - %s\n' "${paths[@]}" >&2
      printf '%s\n' "${paths[0]}"
      return
    fi
  fi

  parse_repo_from_url "$(git remote get-url origin 2>/dev/null || true)" || true
}

parse_repo_from_url() {
  local url="$1"
  local repo_path=""

  case "${url}" in
    git@github.com:*)
      repo_path="${url#git@github.com:}"
      ;;
    https://github.com/*)
      repo_path="${url#https://github.com/}"
      ;;
    *)
      return 1
      ;;
  esac

  repo_path="${repo_path%.git}"
  if [[ "${repo_path}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
    printf '%s\n' "${repo_path}"
    return 0
  fi

  return 1
}

resolve_upstream_path() {
  local is_fork=""
  local remote_upstream_url=""

  if is_fork="$(gh repo view "${fork_path}" --json isFork --jq '.isFork' 2>/dev/null)"; then
    if [[ "${is_fork}" != "true" ]]; then
      echo "Repository ${fork_path} is not marked as a fork on GitHub." >&2
      exit 1
    fi

    upstream_path="$(gh repo view "${fork_path}" --json parent --jq '.parent.nameWithOwner // empty' 2>/dev/null || true)"
    if [[ -n "${upstream_path}" ]]; then
      return
    fi
  fi

  remote_upstream_url="$(git remote get-url upstream 2>/dev/null || true)"
  upstream_path="$(parse_repo_from_url "${remote_upstream_url}" || true)"

  if [[ -z "${upstream_path}" ]]; then
    echo "Could not determine upstream repository for ${fork_path}." >&2
    echo "Ensure the fork still exists on GitHub or that git remote 'upstream' is configured." >&2
    exit 1
  fi
}

repo_exists() {
  gh repo view "$1" >/dev/null 2>&1
}

package_api_path() {
  local owner_type="$1"
  local package_name="$2"

  if [[ "${owner_type}" == "Organization" ]]; then
    printf 'orgs/%s/packages/container/%s\n' "${fork_owner}" "${package_name}"
    return
  fi

  printf 'users/%s/packages/container/%s\n' "${fork_owner}" "${package_name}"
}

delete_container_package_if_present() {
  local owner_type="$1"
  local package_name="$2"
  local package_url="$3"
  local api_path=""

  api_path="$(package_api_path "${owner_type}" "${package_name}")"

  if ! gh api "${api_path}" >/dev/null 2>&1; then
    echo "Container package not found, skipping deletion: ${package_url}"
    return
  fi

  echo "Deleting container package ${package_name}"
  gh api \
    --method DELETE \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2026-03-10" \
    "${api_path}" >/dev/null
}

recreate_fork() {
  local current_login="$1"

  echo "Recreating fork ${fork_path} from ${upstream_path}"
  if [[ "${fork_owner}" == "${current_login}" ]]; then
    gh repo fork "${upstream_path}" --fork-name "${fork_repo}" >/dev/null
    return
  fi

  gh repo fork "${upstream_path}" --org "${fork_owner}" --fork-name "${fork_repo}" >/dev/null
}

require_command gh
require_command git

if ! gh auth status >/dev/null 2>&1; then
  echo "gh CLI is not authenticated" >&2
  exit 1
fi

fork_path="$(prompt_with_default "Fork path (owner/repo)" "$(detect_default_fork_path || true)")"
while [[ ! "${fork_path}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; do
  echo "Invalid fork path '${fork_path}'. Expected format: owner/repo" >&2
  fork_path="$(prompt_with_default "Fork path (owner/repo)" "$(detect_default_fork_path || true)")"
done

fork_owner="${fork_path%%/*}"
fork_repo="${fork_path##*/}"
fork_url="https://github.com/${fork_path}"
package_url="https://github.com/${fork_path}/pkgs/container/${fork_repo}"
package_ci_url="https://github.com/${fork_path}/pkgs/container/${fork_repo}-ci"

if [[ ! -d "${repo_root}/.github/secrets/repos/${fork_path}" ]]; then
  echo "Missing secrets directory: .github/secrets/repos/${fork_path}" >&2
  exit 1
fi

resolve_upstream_path
upstream_url="https://github.com/${upstream_path}"

echo
echo "The following resources will be reset:"
echo "* Fork URL: ${fork_url} (will be deleted)"
echo "* Upstream URL: ${upstream_url} (this repository will be forked again)"
echo "* Container package: ${package_url} (will be deleted)"
echo "* Container package: ${package_ci_url} (will be deleted)"
echo

read -r -p "Type '${fork_path}' to confirm: " confirmation
if [[ "${confirmation}" != "${fork_path}" ]]; then
  echo "Confirmation did not match '${fork_path}'. Aborting." >&2
  exit 1
fi

owner_type="$(gh api "users/${fork_owner}" --jq '.type')"
current_login="$(gh api user --jq '.login')"

delete_container_package_if_present "${owner_type}" "${fork_repo}" "${package_url}"
delete_container_package_if_present "${owner_type}" "${fork_repo}-ci" "${package_ci_url}"

if repo_exists "${fork_path}"; then
  echo "Deleting fork repository ${fork_path}"
  gh repo delete "${fork_path}" --yes
else
  echo "Fork repository not found on GitHub, skipping deletion: ${fork_url}"
fi

recreate_fork "${current_login}"

echo "Running GitHub configuration for ${fork_path}"
"${script_dir}/configure-github.sh" "${fork_path}"

echo "Fetching git remotes"
git fetch --all --prune

echo "Fork reset completed for ${fork_path}"
