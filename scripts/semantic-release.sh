#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

resolve_secrets_dir() {
  local base_dir="${repo_root}/.github/secrets"
  local candidate=""

  if [[ -n "${HYDRA_SECRETS_DIR:-}" ]]; then
    candidate="${HYDRA_SECRETS_DIR}"
    [[ "${candidate}" = /* ]] || candidate="${repo_root}/${candidate}"
    if [[ -d "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return
    fi
  fi

  local repo_slug="${HYDRA_SECRETS_REPO:-${GITHUB_REPOSITORY:-}}"
  if [[ -z "${repo_slug}" ]]; then
    local remote_url
    remote_url="$(git -C "${repo_root}" config --get remote.origin.url 2>/dev/null || true)"
    if [[ "${remote_url}" =~ ^git@github\.com:([^/]+/[^/.]+)(\.git)?$ ]]; then
      repo_slug="${BASH_REMATCH[1]}"
    elif [[ "${remote_url}" =~ ^https://github\.com/([^/]+/[^/.]+)(\.git)?$ ]]; then
      repo_slug="${BASH_REMATCH[1]}"
    fi
  fi

  if [[ -n "${repo_slug}" ]]; then
    candidate="${base_dir}/repos/${repo_slug}"
    if [[ -d "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return
    fi
  fi

  candidate="${base_dir}/repos/hydra-gitops/hydra"
  if [[ -d "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return
  fi

  local discovered_repo_dir
  discovered_repo_dir="$(find "${base_dir}/repos" -mindepth 2 -maxdepth 2 -type d 2>/dev/null | head -n1 || true)"
  if [[ -n "${discovered_repo_dir}" ]]; then
    printf '%s\n' "${discovered_repo_dir}"
    return
  fi

  printf '%s\n' "${base_dir}"
}

extract_git_identity_field() {
  local file_path="$1"
  local section_name="$2"
  local field_name="$3"

  awk -v section="${section_name}" -v field="${field_name}" '
    $0 ~ "^[[:space:]]*" section ":[[:space:]]*$" { in_section=1; next }
    in_section && /^[^[:space:]]/ { in_section=0 }
    in_section && $0 ~ "^[[:space:]]*" field ":[[:space:]]*" {
      value=$0
      sub("^[[:space:]]*" field ":[[:space:]]*", "", value)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if ((value ~ /^".*"$/) || (value ~ /^\047.*\047$/)) {
        value=substr(value, 2, length(value)-2)
      }
      print value
      exit
    }
  ' "${file_path}"
}

secrets_dir="$(resolve_secrets_dir)"
git_public_config_file="${secrets_dir}/git.yaml"

if [[ ! -f "${git_public_config_file}" ]]; then
  echo "Missing required public git config: ${git_public_config_file}" >&2
  exit 1
fi

git_user_name="$(extract_git_identity_field "${git_public_config_file}" "user" "name")"
git_user_email="$(extract_git_identity_field "${git_public_config_file}" "user" "email")"
git_author_name="$(extract_git_identity_field "${git_public_config_file}" "author" "name")"
git_author_email="$(extract_git_identity_field "${git_public_config_file}" "author" "email")"
git_committer_name="$(extract_git_identity_field "${git_public_config_file}" "committer" "name")"
git_committer_email="$(extract_git_identity_field "${git_public_config_file}" "committer" "email")"

git_author_name="${git_author_name:-${git_user_name}}"
git_author_email="${git_author_email:-${git_user_email}}"
git_committer_name="${git_committer_name:-${git_user_name}}"
git_committer_email="${git_committer_email:-${git_user_email}}"

if [[ -z "${git_user_name}" || "${git_user_name}" == "null" ]]; then
  echo "Missing required user.name in ${git_public_config_file}" >&2
  exit 1
fi

if [[ -z "${git_user_email}" || "${git_user_email}" == "null" ]]; then
  echo "Missing required user.email in ${git_public_config_file}" >&2
  exit 1
fi

: "${SOPS_AGE_KEY_RELEASE:=${SOPS_AGE_KEY:-}}"
if [[ -z "${SOPS_AGE_KEY_RELEASE}" ]]; then
  SOPS_AGE_KEY_RELEASE="$(sops --decrypt --extract '["age_keys"]["release"]["private_key"]' "${secrets_dir}/age-pipeline-keys.sops.yaml" 2>/dev/null | grep AGE-SECRET-KEY-)"
fi
if [[ -z "${SOPS_AGE_KEY_RELEASE}" ]]; then
  echo "SOPS_AGE_KEY_RELEASE or SOPS_AGE_KEY must be set" >&2
  exit 1
fi

export SOPS_AGE_KEY="${SOPS_AGE_KEY_RELEASE}"

tmp_dir="${RUNNER_TEMP:-/tmp}"
mkdir -p "${tmp_dir}"
git_signing_key="$(mktemp "${tmp_dir}/hydra_git_signing.XXXXXX.key")"
allowed_signers_file="$(mktemp "${tmp_dir}/hydra_allowed_signers.XXXXXX")"
git_signing_key_raw="$(sops --decrypt --extract '["git_signing"]["private_key"]' "${secrets_dir}/git.sops.yaml")"
if [[ "${git_signing_key_raw}" == *\\n* ]]; then
  printf '%b\n' "${git_signing_key_raw}" > "${git_signing_key}"
else
  printf '%s\n' "${git_signing_key_raw}" > "${git_signing_key}"
fi
chmod 600 "${git_signing_key}"

cleanup() {
  rm -f "${git_signing_key}" "${git_signing_key}.pub" "${allowed_signers_file}"
  rm -f "${semantic_release_log:-}"
  if [[ -n "${git_wrapper_dir:-}" ]]; then
    rm -rf "${git_wrapper_dir}"
  fi
}
trap cleanup EXIT

if ! ssh-keygen -y -P "" -f "${git_signing_key}" >/dev/null 2>&1; then
  echo "Extracted git_signing.private_key is invalid or requires a passphrase; CI needs an unencrypted SSH signing key" >&2
  exit 1
fi

git_signing_pub="$(ssh-keygen -y -P "" -f "${git_signing_key}")"
printf '%s %s\n' "${git_user_email}" "${git_signing_pub}" > "${allowed_signers_file}"

export GIT_AUTHOR_NAME="${git_author_name}"
export GIT_AUTHOR_EMAIL="${git_author_email}"
export GIT_COMMITTER_NAME="${git_committer_name}"
export GIT_COMMITTER_EMAIL="${git_committer_email}"

# Inject signing config for all git invocations in this process tree without
# touching repository-local git config.
export GIT_CONFIG_COUNT=11
export GIT_CONFIG_KEY_0="user.name"
export GIT_CONFIG_VALUE_0="${git_user_name}"
export GIT_CONFIG_KEY_1="user.email"
export GIT_CONFIG_VALUE_1="${git_user_email}"
export GIT_CONFIG_KEY_2="author.name"
export GIT_CONFIG_VALUE_2="${git_author_name}"
export GIT_CONFIG_KEY_3="author.email"
export GIT_CONFIG_VALUE_3="${git_author_email}"
export GIT_CONFIG_KEY_4="committer.name"
export GIT_CONFIG_VALUE_4="${git_committer_name}"
export GIT_CONFIG_KEY_5="committer.email"
export GIT_CONFIG_VALUE_5="${git_committer_email}"
export GIT_CONFIG_KEY_6="gpg.format"
export GIT_CONFIG_VALUE_6="ssh"
export GIT_CONFIG_KEY_7="user.signingkey"
export GIT_CONFIG_VALUE_7="${git_signing_key}"
export GIT_CONFIG_KEY_8="commit.gpgsign"
export GIT_CONFIG_VALUE_8="true"
export GIT_CONFIG_KEY_9="tag.gpgSign"
export GIT_CONFIG_VALUE_9="false"
export GIT_CONFIG_KEY_10="gpg.ssh.allowedSignersFile"
export GIT_CONFIG_VALUE_10="${allowed_signers_file}"
export GIT_TERMINAL_PROMPT="0"
export GIT_EDITOR=:

log_release() {
  printf '[semantic-release.sh] %s\n' "$*" >&2
}

real_git_bin="$(command -v git)"
git_wrapper_dir="$(mktemp -d "${tmp_dir}/hydra_git_wrapper.XXXXXX")"
git_bin="${git_wrapper_dir}/git-bin"
cat > "${git_bin}" <<EOF
#!/usr/bin/env bash
set -euo pipefail

printf '[git-bin] cwd=%s cmd=%q' "\$(pwd)" "${real_git_bin}" >&2
for arg in "\$@"; do
  printf ' %q' "\${arg}" >&2
done
printf '\n' >&2

exec "${real_git_bin}" "\$@"
EOF
chmod 700 "${git_bin}"

cat > "${git_wrapper_dir}/git" <<EOF
#!/usr/bin/env bash
set -euo pipefail

log_git_wrapper() {
  printf '[git-wrapper] cwd=%s cmd=git' "\$(pwd)" >&2
  for arg in "\$@"; do
    printf ' %q' "\${arg}" >&2
  done
  printf '\n' >&2
}

log_git_wrapper "\$@"
exec "${git_bin}" "\$@"
EOF
chmod 700 "${git_wrapper_dir}/git"
export PATH="${git_wrapper_dir}:${PATH}"
export DEBUG="*"

repository_url=""
if [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
  github_server_url="${GITHUB_SERVER_URL:-https://github.com}"
  repository_url="${github_server_url%/}/${GITHUB_REPOSITORY}.git"
fi

if [[ -z "${repository_url}" ]]; then
  repository_url="$(git -C "${repo_root}" config --get remote.origin.url 2>/dev/null || true)"
fi

if [[ -z "${repository_url}" ]]; then
  echo "Could not determine repository URL for semantic-release" >&2
  exit 1
fi

log_release "using semantic-release repository URL: ${repository_url}"
log_release "starting semantic-release"

semantic_release_log="$(mktemp "${tmp_dir}/hydra_semantic_release.XXXXXX.log")"
semantic_release_exit_code=0

npx -y \
  -p semantic-release@25.0.3 \
  -p conventional-changelog-conventionalcommits \
  -p @semantic-release/changelog \
  -p @semantic-release/exec \
  -p @semantic-release/git \
  semantic-release --repository-url "${repository_url}" --debug 2>&1 | tee "${semantic_release_log}" || semantic_release_exit_code=$?

if [[ ${semantic_release_exit_code} -ne 0 ]]; then
  if grep -Eq "fatal: tag 'v[0-9]+\.[0-9]+\.[0-9]+[^']*' already exists" "${semantic_release_log}"; then
    log_release "release tag already exists; treating rerun as successful"
  else
    exit "${semantic_release_exit_code}"
  fi
fi

log_release "semantic-release finished, continuing publish pipeline"
