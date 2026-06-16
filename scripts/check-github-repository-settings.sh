#!/usr/bin/env bash

set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "gh CLI is not authenticated" >&2
  exit 1
fi

repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq '.nameWithOwner')}"
default_branch="${DEFAULT_BRANCH:-$(gh repo view --json defaultBranchRef --jq '.defaultBranchRef.name')}"
owner="${repo%/*}"
name="${repo#*/}"
full_ref="refs/heads/${default_branch}"

failures=0
env_names=()
matched_branch_protection_rules=()
matched_rulesets=()

pass() {
  echo "OK: $1"
}

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

log_detail() {
  echo "  - $1" >&2
}

skip() {
  echo "SKIP: $1"
}

is_github_actions() {
  [[ "${GITHUB_ACTIONS:-}" == "true" ]]
}

contains_line() {
  local needle="$1"
  shift
  local line
  for line in "$@"; do
    if [[ "${line}" == "${needle}" ]]; then
      return 0
    fi
  done
  return 1
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

check_signed_commits_with_branch_protection() {
  local pattern
  local requires
  local saw_match=0

  matched_branch_protection_rules=()

  while IFS='|' read -r pattern requires; do
    [[ -z "${pattern:-}" ]] && continue

    if matches_ref_pattern "${default_branch}" "${full_ref}" "${pattern}"; then
      saw_match=1
      matched_branch_protection_rules+=("pattern=${pattern} requiresCommitSignatures=${requires}")
      if [[ "${requires}" == "true" ]]; then
        return 0
      fi
    fi
  done < <(
    gh api graphql \
      -F owner="${owner}" \
      -F name="${name}" \
      -f query='
        query($owner: String!, $name: String!) {
          repository(owner: $owner, name: $name) {
            branchProtectionRules(first: 100) {
              nodes {
                pattern
                requiresCommitSignatures
              }
            }
          }
        }
      ' \
      --jq '.data.repository.branchProtectionRules.nodes[]? | [.pattern, (.requiresCommitSignatures | tostring)] | join("|")'
  )

  [[ "${saw_match}" -eq 1 ]] && return 1
  return 2
}

check_signed_commits_with_rulesets() {
  local ruleset_name
  local target
  local enforcement
  local target_normalized
  local enforcement_normalized
  local includes
  local excludes
  local rules
  local rules_normalized
  local include_pattern
  local exclude_pattern
  local include_match
  local exclude_match
  local saw_applicable=0

  matched_rulesets=()

  while IFS='|' read -r ruleset_name target enforcement includes excludes rules; do
    [[ -z "${target:-}" ]] && continue
    target_normalized="${target^^}"
    enforcement_normalized="${enforcement^^}"
    [[ "${target_normalized}" == "BRANCH" ]] || continue
    [[ "${enforcement_normalized}" == "ACTIVE" ]] || continue

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

    exclude_match=0
    IFS=',' read -r -a exclude_patterns <<< "${excludes}"
    for exclude_pattern in "${exclude_patterns[@]}"; do
      [[ -z "${exclude_pattern}" ]] && continue
      if matches_ref_pattern "${default_branch}" "${full_ref}" "${exclude_pattern}"; then
        exclude_match=1
        break
      fi
    done
    [[ "${exclude_match}" -eq 0 ]] || continue

    saw_applicable=1
    rules_normalized="${rules^^}"
    matched_rulesets+=("name=${ruleset_name} include=${includes:-<none>} exclude=${excludes:-<none>} rules=${rules:-<none>}")
    if [[ ",${rules_normalized}," == *",REQUIRED_SIGNATURES,"* ]]; then
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

  [[ "${saw_applicable}" -eq 1 ]] && return 1
  return 2
}

print_signed_commit_diagnostics() {
  if [[ "${#matched_branch_protection_rules[@]}" -gt 0 ]]; then
    echo "Matching branch protection rules for ${default_branch}:" >&2
    printf '%s\n' "${matched_branch_protection_rules[@]}" | while IFS= read -r rule; do
      log_detail "${rule}"
    done
  fi

  if [[ "${#matched_rulesets[@]}" -gt 0 ]]; then
    echo "Applicable active rulesets for ${default_branch}:" >&2
    printf '%s\n' "${matched_rulesets[@]}" | while IFS= read -r ruleset; do
      log_detail "${ruleset}"
    done
  fi
}

check_environment_presence() {
  mapfile -t env_names < <(gh api "repos/${repo}/environments" --jq '.environments[]?.name')

  if [[ "${#env_names[@]}" -lt 2 ]]; then
    fail "Expected at least two environments, found ${#env_names[@]}"
    return
  fi

  if contains_line "publish" "${env_names[@]}" && contains_line "release" "${env_names[@]}"; then
    pass "Environments publish and release exist"
  else
    fail "Expected environments publish and release"
  fi
}

environment_exists() {
  local environment="$1"
  contains_line "${environment}" "${env_names[@]}"
}

check_environment_branch_policy() {
  local environment="$1"
  local expected_name="$2"
  local expected_type="$3"

  local count
  local actual_name
  local actual_type

  if ! count="$(gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" --jq '.total_count')"; then
    fail "Could not read deployment branch policies for environment ${environment}"
    return
  fi

  if [[ "${count}" != "1" ]]; then
    fail "Environment ${environment} must have exactly one deployment branch policy, found ${count}"
    return
  fi

  actual_name="$(gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" --jq '.branch_policies[0].name')"
  actual_type="$(gh api "repos/${repo}/environments/${environment}/deployment-branch-policies" --jq '.branch_policies[0].type')"

  if [[ "${actual_name}" == "${expected_name}" && "${actual_type}" == "${expected_type}" ]]; then
    pass "Environment ${environment} points to ${expected_type} ${expected_name}"
  else
    fail "Environment ${environment} must point to ${expected_type} ${expected_name}, found ${actual_type} ${actual_name}"
  fi
}

check_environment_secret() {
  local environment="$1"
  local expected_secret="$2"

  local count
  local actual_secret
  local secrets_output
  local secrets_status

  set +e
  secrets_output="$(gh api "repos/${repo}/environments/${environment}/secrets" 2>&1)"
  secrets_status=$?
  set -e

  if [[ "${secrets_status}" -ne 0 ]]; then
    if is_github_actions && [[ "${secrets_output}" == *"Resource not accessible by integration"* ]]; then
      skip "Could not read secrets for environment ${environment} in GitHub Actions with GITHUB_TOKEN"
      return
    fi

    fail "Could not read secrets for environment ${environment}"
    return
  fi

  count="$(jq -r '.total_count' <<< "${secrets_output}")"

  if [[ "${count}" != "1" ]]; then
    fail "Environment ${environment} must have exactly one secret, found ${count}"
    return
  fi

  actual_secret="$(jq -r '.secrets[0].name' <<< "${secrets_output}")"

  if [[ "${actual_secret}" == "${expected_secret}" ]]; then
    pass "Environment ${environment} has only secret ${expected_secret}"
  else
    fail "Environment ${environment} must have only secret ${expected_secret}, found ${actual_secret}"
  fi
}

check_signed_commits() {
  local branch_protection_status
  local ruleset_status

  if check_signed_commits_with_branch_protection; then
    pass "Signed commits are required for ${default_branch} via branch protection"
    return
  else
    branch_protection_status=$?
  fi

  if check_signed_commits_with_rulesets; then
    pass "Signed commits are required for ${default_branch} via ruleset"
    return
  else
    ruleset_status=$?
  fi

  if [[ "${branch_protection_status}" == "1" || "${ruleset_status}" == "1" ]]; then
    fail "Signed commits are not required for ${default_branch}"
    print_signed_commit_diagnostics
    return
  fi

  case "${branch_protection_status}:${ruleset_status}" in
    2:2)
      fail "No branch protection or ruleset requiring signed commits applies to ${default_branch}"
      ;;
    *)
      fail "Signed commits are not required for ${default_branch}"
      print_signed_commit_diagnostics
      ;;
  esac
}

echo "Checking GitHub repository settings for ${repo}"

check_signed_commits
check_environment_presence

if environment_exists "publish"; then
  check_environment_branch_policy "publish" "v*" "tag"
  check_environment_secret "publish" "SOPS_AGE_KEY_PUBLISH"
fi

if environment_exists "release"; then
  check_environment_branch_policy "release" "main" "branch"
  check_environment_secret "release" "SOPS_AGE_KEY_RELEASE"
fi

if [[ "${failures}" -gt 0 ]]; then
  echo "Checks failed: ${failures}" >&2
  exit 1
fi

echo "All checks passed"
