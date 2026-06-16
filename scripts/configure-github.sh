#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/configure-github.sh <owner/repo> [fork-secrets-dir]

Examples:
  scripts/configure-github.sh drieks/hydra
  scripts/configure-github.sh drieks/hydra .github/secrets/repos/drieks/hydra

This script configures GitHub Actions environments and secrets to match
scripts/check-github-repository-settings.sh checks.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -lt 1 || $# -gt 2 ]]; then
  usage >&2
  exit 1
fi

repo="$1"

if [[ ! "${repo}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  echo "Invalid repo '${repo}'. Expected format: owner/repo" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

fork_secrets_dir_rel="${2:-.github/secrets/repos/${repo}}"
fork_secrets_dir="${repo_root}/${fork_secrets_dir_rel}"
age_keys_file="${fork_secrets_dir}/age-pipeline-keys.sops.yaml"
public_keys_file="${fork_secrets_dir}/public-keys.yaml"

hydra_bin=""
signed_commits_ruleset_name="Hydra Require Signed Commits"

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "${cmd} is required" >&2
    exit 1
  fi
}

warn() {
  echo "WARN: $1" >&2
}

is_github_actions() {
  [[ "${GITHUB_ACTIONS:-}" == "true" ]]
}

resolve_hydra_bin() {
  if command -v hydra >/dev/null 2>&1; then
    hydra_bin="hydra"
    return
  fi

  if [[ -x "${repo_root}/hydra-go/hydra" ]]; then
    hydra_bin="${repo_root}/hydra-go/hydra"
    return
  fi

  if [[ -x "${repo_root}/hydra-go/hydra.sh" ]]; then
    hydra_bin="${repo_root}/hydra-go/hydra.sh"
    return
  fi

  echo "hydra binary is required (used for 'hydra yq')" >&2
  exit 1
}

delete_existing_branch_policies() {
  local environment="$1"
  local policy_id

  while IFS= read -r policy_id; do
    [[ -z "${policy_id}" ]] && continue
    gh api --method DELETE "repos/${repo}/environments/${environment}/deployment-branch-policies/${policy_id}" >/dev/null
  done < <(gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" --jq '.branch_policies[]?.id')
}

set_environment_secret_from_age_file() {
  local environment="$1"
  local secret_name="$2"
  local yq_path="$3"

  gh secret set "${secret_name}" --env "${environment}" --repo "${repo}" < <(
    sops -d "${age_keys_file}" | "${hydra_bin}" yq eval -r "${yq_path}" -
  )
}

ensure_user_ssh_signing_key() {
  local signing_key="$1"
  local signing_key_title="$2"
  local existing_key=""
  local list_output
  local list_status
  local create_output
  local create_status

  if is_github_actions; then
    warn "Skipping SSH signing key registration in GitHub Actions."
    warn "Use a personal 'gh auth login' session with scope admin:ssh_signing_key to register it once."
    return
  fi

  set +e
  list_output="$(gh api user/ssh_signing_keys --paginate --jq '.[].key' 2>&1)"
  list_status=$?
  set -e

  if [[ "${list_status}" -ne 0 ]]; then
    if [[ "${list_output}" == *"admin:ssh_signing_key"* ]]; then
      warn "Cannot read SSH signing keys (missing scope admin:ssh_signing_key)."
      warn "Run: gh auth refresh -h github.com -s admin:ssh_signing_key"
      warn "Continuing without auto-registering the SSH signing key."
      return
    fi

    if [[ "${list_output}" == *"Not Found"* ]]; then
      warn "SSH signing key API endpoint is not available for this account/token (HTTP 404)."
      warn "Continuing without auto-registering the SSH signing key."
      return
    fi

    echo "Could not query existing SSH signing keys." >&2
    echo "gh output: ${list_output}" >&2
    exit 1
  fi

  while IFS= read -r existing_key; do
    if [[ "${existing_key}" == "${signing_key}" ]]; then
      return
    fi
  done <<< "${list_output}"

  set +e
  create_output="$(gh api --method POST user/ssh_signing_keys \
    -f title="${signing_key_title}" \
    -f key="${signing_key}" 2>&1)"
  create_status=$?
  set -e

  if [[ "${create_status}" -eq 0 ]]; then
    return
  fi

  if [[ "${create_output}" == *"admin:ssh_signing_key"* ]]; then
    warn "Could not register SSH signing key (missing scope admin:ssh_signing_key)."
    warn "Run: gh auth refresh -h github.com -s admin:ssh_signing_key"
    warn "Continuing without auto-registering the SSH signing key."
    return
  fi

  if [[ "${create_output}" == *"Not Found"* ]]; then
    warn "Could not register SSH signing key via GitHub API endpoint user/ssh_signing_keys (HTTP 404)."
    warn "Continuing without auto-registering the SSH signing key."
    return
  fi

  echo "Could not register SSH signing key on the authenticated GitHub user." >&2
  echo "Ensure 'gh auth login' uses a token with permission to manage SSH signing keys (for classic PAT: admin:ssh_signing_key)." >&2
  echo "gh output: ${create_output}" >&2
  exit 1
}

ensure_release_signing_key_accepted() {
  local signing_key
  local current_user

  signing_key="$("${hydra_bin}" yq eval -r '.git_signing.public_key_openssh' "${public_keys_file}")"
  if [[ -z "${signing_key}" || "${signing_key}" == "null" ]]; then
    echo "Could not read git_signing.public_key_openssh from ${public_keys_file}" >&2
    exit 1
  fi

  current_user="$(gh api user --jq '.login' 2>/dev/null || true)"
  if [[ -z "${current_user}" || "${current_user}" == "null" ]]; then
    echo "Could not determine authenticated GitHub user via gh api user" >&2
    exit 1
  fi

  ensure_user_ssh_signing_key "${signing_key}" "Hydra Release Signing (${repo})"
}

matches_ref_pattern() {
  local short_ref="$1"
  local full_branch_ref="$2"
  local pattern="$3"

  case "${pattern}" in
    "~ALL")
      return 0
      ;;
    "~DEFAULT_BRANCH")
      [[ "${short_ref}" == "${default_branch}" ]]
      return
      ;;
    refs/heads/* | refs/tags/*)
      [[ "${full_branch_ref}" == ${pattern} ]]
      return
      ;;
    *)
      [[ "${short_ref}" == ${pattern} ]]
      return
      ;;
  esac
}

ruleset_requires_signed_commits_for_default_branch() {
  local full_ref="$1"
  local ruleset_name
  local target
  local enforcement
  local includes
  local excludes
  local rules
  local include_pattern
  local exclude_pattern
  local include_match

  while IFS='|' read -r ruleset_name target enforcement includes excludes rules; do
    [[ -z "${target:-}" ]] && continue
    [[ "${target^^}" == "BRANCH" ]] || continue
    [[ "${enforcement^^}" == "ACTIVE" ]] || continue

    include_match=0
    IFS=',' read -r -a include_patterns <<< "${includes}"
    for include_pattern in "${include_patterns[@]}"; do
      [[ -z "${include_pattern}" ]] && continue
      if matches_ref_pattern "${default_branch}" "${full_ref}" "${include_pattern}"; then
        include_match=1
        break
      fi
    done
    [[ "${include_match}" -eq 1 ]] || continue

    IFS=',' read -r -a exclude_patterns <<< "${excludes}"
    for exclude_pattern in "${exclude_patterns[@]}"; do
      [[ -z "${exclude_pattern}" ]] && continue
      if matches_ref_pattern "${default_branch}" "${full_ref}" "${exclude_pattern}"; then
        include_match=0
        break
      fi
    done
    [[ "${include_match}" -eq 1 ]] || continue

    if [[ ",${rules^^}," == *",REQUIRED_SIGNATURES,"* ]]; then
      return 0
    fi
  done < <(
    gh api graphql \
      -F owner="${owner}" \
      -F name="${name}" \
      -f query='
        query($owner: String!, $name: String!) {
          repository(owner: $owner, name: $name) {
            rulesets(first: 100) {
              nodes {
                name
                target
                enforcement
                conditions {
                  refName {
                    include
                    exclude
                  }
                }
                rules(first: 100) {
                  nodes {
                    type
                  }
                }
              }
            }
          }
        }
      ' \
      --jq '.data.repository.rulesets.nodes[]? | [(.name // "<unnamed>"), .target, .enforcement, (((.conditions.refName.include // []) | join(",")) // ""), (((.conditions.refName.exclude // []) | join(",")) // ""), ((([.rules.nodes[]?.type] // []) | join(",")) // "")] | join("|")'
  )

  return 1
}

ensure_github_actions_enabled() {
  gh api --method PUT "repos/${repo}/actions/permissions" --input - >/dev/null <<'JSON'
{
  "enabled": true,
  "allowed_actions": "all"
}
JSON

  gh api --method PUT "repos/${repo}/actions/permissions/workflow" --input - >/dev/null <<'JSON'
{
  "default_workflow_permissions": "write",
  "can_approve_pull_request_reviews": true
}
JSON
}

ensure_signed_commits_ruleset() {
  local full_ref
  local include_ref
  local ruleset_id
  local created_id

  default_branch="$(gh repo view "${repo}" --json defaultBranchRef --jq '.defaultBranchRef.name')"
  full_ref="refs/heads/${default_branch}"
  include_ref="refs/heads/${default_branch}"

  # Keep this idempotent by replacing only the ruleset managed by this script.
  while IFS= read -r ruleset_id; do
    [[ -z "${ruleset_id}" ]] && continue
    gh api --method DELETE "repos/${repo}/rulesets/${ruleset_id}" >/dev/null
  done < <(gh api "repos/${repo}/rulesets" --jq ".[]? | select(.name == \"${signed_commits_ruleset_name}\") | .id")

  created_id="$(gh api --method POST "repos/${repo}/rulesets" --input - --jq '.id' <<JSON
{
  "name": "${signed_commits_ruleset_name}",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "include": ["${include_ref}"],
      "exclude": []
    }
  },
  "rules": [
    {
      "type": "required_signatures"
    }
  ]
}
JSON
)"

  if [[ -z "${created_id}" || "${created_id}" == "null" ]]; then
    echo "Could not create signed commits ruleset for ${default_branch}" >&2
    exit 1
  fi

  if ! ruleset_requires_signed_commits_for_default_branch "${full_ref}"; then
    echo "Signed-commit ruleset was created but does not apply to ${default_branch}." >&2
    echo "Verify repository rulesets and permissions in GitHub settings." >&2
    exit 1
  fi
}

require_command gh
require_command sops
resolve_hydra_bin

if ! gh auth status >/dev/null 2>&1; then
  echo "gh CLI is not authenticated" >&2
  exit 1
fi

if [[ ! -f "${age_keys_file}" ]]; then
  echo "Missing file: ${age_keys_file}" >&2
  exit 1
fi

if [[ ! -f "${public_keys_file}" ]]; then
  echo "Missing file: ${public_keys_file}" >&2
  exit 1
fi

echo "Configuring GitHub settings for ${repo}"

owner="${repo%/*}"
name="${repo#*/}"
default_branch="$(gh repo view "${repo}" --json defaultBranchRef --jq '.defaultBranchRef.name')"

echo "Ensuring GitHub Actions is enabled"
ensure_github_actions_enabled

echo "Ensuring environments exist and allow custom branch policies"
gh api --method PUT "repos/${repo}/environments/publish" --input - >/dev/null <<'JSON'
{
  "deployment_branch_policy": {
    "protected_branches": false,
    "custom_branch_policies": true
  }
}
JSON
gh api --method PUT "repos/${repo}/environments/release" --input - >/dev/null <<'JSON'
{
  "deployment_branch_policy": {
    "protected_branches": false,
    "custom_branch_policies": true
  }
}
JSON

echo "Applying deployment branch policies"
delete_existing_branch_policies "publish"
delete_existing_branch_policies "release"
gh api --method POST "repos/${repo}/environments/publish/deployment-branch-policies" -f name='v*' -f type='tag' >/dev/null
gh api --method POST "repos/${repo}/environments/release/deployment-branch-policies" -f name='main' -f type='branch' >/dev/null

echo "Setting environment secrets"
set_environment_secret_from_age_file "publish" "SOPS_AGE_KEY_PUBLISH" '.age_keys.publish.private_key'
set_environment_secret_from_age_file "release" "SOPS_AGE_KEY_RELEASE" '.age_keys.release.private_key'

echo "Ensuring release SSH signing key is registered for the authenticated GitHub user"
ensure_release_signing_key_accepted

echo "Ensuring signed commits are required on the default branch"
ensure_signed_commits_ruleset

echo "Running settings validation"
GITHUB_REPOSITORY="${repo}" "${script_dir}/check-github-repository-settings.sh"

echo "GitHub configuration completed for ${repo}"
