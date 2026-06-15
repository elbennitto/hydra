#!/usr/bin/env bash

set -euo pipefail

on_error() {
  local rc=$?
  echo "ERROR: command failed with exit code ${rc} at line ${BASH_LINENO[0]}: ${BASH_COMMAND}" >&2
  exit "${rc}"
}
trap on_error ERR

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
tmp_dir="${RUNNER_TEMP:-/tmp}"

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

secrets_dir="$(resolve_secrets_dir)"

tag=""
version_core=""
version_major=""
version_minor=""
cosign_key=""
allowed_signers=""
container_context=""
homebrew_formula_context=""
homebrew_tap_deploy_key=""
homebrew_tap_deploy_target_repo=""
homebrew_tap_deploy_target_owner=""
homebrew_tap_deploy_target_name=""
git_signing_key=""
git_signing_pub=""
git_signing_allowed_signers=""
git_user_name=""
git_user_email=""
git_author_name=""
git_author_email=""
git_committer_name=""
git_committer_email=""

cleanup() {
  rm -f "${cosign_key:-}" "${allowed_signers:-}" "${homebrew_tap_deploy_key:-}"
  rm -f "${git_signing_key:-}" "${git_signing_pub:-}" "${git_signing_allowed_signers:-}"
  rm -rf "${container_context:-}" "${homebrew_formula_context:-}"
}
trap cleanup EXIT

sha256_file() {
  local file_path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file_path}" | awk '{print $1}'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file_path}" | awk '{print $1}'
    return
  fi

  echo "Neither sha256sum nor shasum is available" >&2
  exit 1
}

resolve_tag() {
  tag="${RELEASE_TAG:-${GITHUB_REF_NAME:-}}"
  if [[ -z "${tag}" ]]; then
    echo "Tag is required (RELEASE_TAG or GITHUB_REF_NAME)" >&2
    exit 1
  fi

  if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]; then
    echo "Tag ${tag} is not a semantic vX.Y.Z tag" >&2
    exit 1
  fi

  local normalized_tag
  normalized_tag="${tag#v}"
  version_core="${normalized_tag%%[-+]*}"
  IFS='.' read -r version_major version_minor _ <<<"${version_core}"
}

container_tags_for_release() {
  resolve_tag

  local tags=("${tag}")
  if [[ "${tag}" == "v${version_core}" ]]; then
    tags+=("v${version_major}.${version_minor}" "v${version_major}" "latest")
  fi

  printf '%s\n' "${tags[@]}"
}

ensure_sops_key() {
  : "${SOPS_AGE_KEY_PUBLISH:=${SOPS_AGE_KEY:-}}"
  if [[ -z "${SOPS_AGE_KEY_PUBLISH}" ]]; then
    SOPS_AGE_KEY_PUBLISH="$(sops --decrypt --extract '["age_keys"]["publish"]["private_key"]' "${secrets_dir}/age-pipeline-keys.sops.yaml" 2>/dev/null | grep AGE-SECRET-KEY- || true)"
  fi
  if [[ -z "${SOPS_AGE_KEY_PUBLISH}" ]]; then
    echo "SOPS_AGE_KEY_PUBLISH or SOPS_AGE_KEY must be set" >&2
    exit 1
  fi

  export SOPS_AGE_KEY="${SOPS_AGE_KEY_PUBLISH}"
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

load_git_identity_from_config() {
  local git_public_config_file="${secrets_dir}/git.yaml"

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

  if [[ -z "${git_user_name}" || "${git_user_name}" == "null" || -z "${git_user_email}" || "${git_user_email}" == "null" ]]; then
    echo "Could not read required user.name/user.email from ${git_public_config_file}" >&2
    exit 1
  fi

  git_author_name="${git_author_name:-${git_user_name}}"
  git_author_email="${git_author_email:-${git_user_email}}"
  git_committer_name="${git_committer_name:-${git_user_name}}"
  git_committer_email="${git_committer_email:-${git_user_email}}"
}

verify() {
  resolve_tag
  ensure_sops_key
  load_git_identity_from_config

  mkdir -p "${tmp_dir}"

  local pub_openssh tag_commit tagger_name tagger_email expected_tagger_name expected_tagger_email
  expected_tagger_name="${git_user_name}"
  expected_tagger_email="${git_user_email}"

  pub_openssh="$(awk -F": " '/public_key_openssh:/{gsub(/^"|"$/, "", $2); print $2}' "${secrets_dir}/public-keys.yaml")"
  if [[ -z "${pub_openssh}" ]]; then
    echo "Could not read git_signing.public_key_openssh from ${secrets_dir}/public-keys.yaml" >&2
    exit 1
  fi

  allowed_signers="${tmp_dir}/allowed_signers"
  echo "${pub_openssh}" | awk -v principal="${expected_tagger_email}" '{print principal" "$1" "$2}' > "${allowed_signers}"

  git config gpg.format ssh
  git config gpg.ssh.allowedSignersFile "${allowed_signers}"
  git fetch origin main --force
  git fetch --tags --force

  git tag -v "${tag}"

  tag_commit="$(git rev-list -n1 "${tag}")"
  if [[ -z "${tag_commit}" ]]; then
    echo "Could not resolve commit for tag ${tag}" >&2
    exit 1
  fi

  if ! git merge-base --is-ancestor "${tag_commit}" origin/main; then
    echo "Tag ${tag} does not point to a commit on origin/main" >&2
    exit 1
  fi

  tagger_name="$(git for-each-ref "refs/tags/${tag}" --format='%(taggername)')"
  tagger_email="$(git for-each-ref "refs/tags/${tag}" --format='%(taggeremail)' | tr -d '<>')"
  if [[ "${tagger_name}" != "${expected_tagger_name}" || "${tagger_email}" != "${expected_tagger_email}" ]]; then
    echo "Tag ${tag} was not created by the expected release identity" >&2
    echo "Found tagger name='${tagger_name}' email='${tagger_email}'" >&2
    echo "Expected tagger name='${expected_tagger_name}' email='${expected_tagger_email}'" >&2
    exit 1
  fi
}

load_publish_secrets() {
  ensure_sops_key

  local publish_public_config_file="${secrets_dir}/publish.yaml"
  if [[ ! -f "${publish_public_config_file}" ]]; then
    echo "Missing required publish config: ${publish_public_config_file}" >&2
    exit 1
  fi

  homebrew_tap_deploy_target_repo="$(awk '
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
  ' "${publish_public_config_file}")"

  if [[ -z "${homebrew_tap_deploy_target_repo}" || "${homebrew_tap_deploy_target_repo}" == "null" ]]; then
    echo "Missing required homebrew.tap_deploy_target_repo in ${publish_public_config_file}" >&2
    exit 1
  fi

  if [[ ! "${homebrew_tap_deploy_target_repo}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
    echo "Invalid homebrew.tap_deploy_target_repo in ${publish_public_config_file}: ${homebrew_tap_deploy_target_repo}" >&2
    echo "Expected value format: owner/repo" >&2
    exit 1
  fi

  homebrew_tap_deploy_target_owner="${homebrew_tap_deploy_target_repo%%/*}"
  homebrew_tap_deploy_target_name="${homebrew_tap_deploy_target_repo##*/}"

  export HOMEBREW_TAP_DEPLOY_TARGET_REPO="${homebrew_tap_deploy_target_repo}"
  export HOMEBREW_TAP_DEPLOY_TARGET_OWNER="${homebrew_tap_deploy_target_owner}"
  export HOMEBREW_TAP_DEPLOY_TARGET_NAME="${homebrew_tap_deploy_target_name}"

  mkdir -p "${tmp_dir}"
  cosign_key="${tmp_dir}/cosign.key"
  sops --decrypt --extract '["cosign"]["private_key"]' "${secrets_dir}/publish.sops.yaml" > "${cosign_key}"
  chmod 600 "${cosign_key}"
  COSIGN_PASSWORD="$(sops --decrypt --extract '["cosign"]["password"]' "${secrets_dir}/publish.sops.yaml")"

  # Prefer SSH deploy key auth for homebrew tap updates.
  # Also accept legacy tap_token field as fallback during migration.
  local tap_deploy_key
  tap_deploy_key="$(sops --decrypt --extract '["homebrew"]["tap_deploy_key"]' "${secrets_dir}/publish.sops.yaml" 2>/dev/null || true)"
  if [[ -z "${tap_deploy_key}" ]]; then
    tap_deploy_key="$(sops --decrypt --extract '["homebrew"]["tap_token"]' "${secrets_dir}/publish.sops.yaml" 2>/dev/null || true)"
  fi
  if [[ -n "${tap_deploy_key}" ]]; then
    homebrew_tap_deploy_key="${tmp_dir}/homebrew_tap_deploy_key"
    printf "%s\n" "${tap_deploy_key}" > "${homebrew_tap_deploy_key}"
    chmod 600 "${homebrew_tap_deploy_key}"
    export HOMEBREW_TAP_DEPLOY_KEY="${homebrew_tap_deploy_key}"
  else
    echo "Could not load homebrew tap deploy key from publish secrets" >&2
    exit 1
  fi

  export COSIGN_PASSWORD
  export COSIGN_PRIVATE_KEY_PATH="${cosign_key}"
}

load_git_release_identity() {
  ensure_sops_key

  if [[ -n "${git_signing_key}" ]]; then
    return
  fi

  load_git_identity_from_config

  mkdir -p "${tmp_dir}"

  git_signing_key="${tmp_dir}/hydra_git_signing.key"
  git_signing_allowed_signers="${tmp_dir}/hydra_git_allowed_signers"

  local git_signing_key_raw
  git_signing_key_raw="$(sops --decrypt --extract '["git_signing"]["private_key"]' "${secrets_dir}/git.sops.yaml")"
  if [[ "${git_signing_key_raw}" == *\\n* ]]; then
    printf '%b\n' "${git_signing_key_raw}" > "${git_signing_key}"
  else
    printf '%s\n' "${git_signing_key_raw}" > "${git_signing_key}"
  fi
  chmod 600 "${git_signing_key}"

  if ! ssh-keygen -y -P "" -f "${git_signing_key}" >/dev/null 2>&1; then
    echo "Extracted git_signing.private_key is invalid or requires a passphrase; CI needs an unencrypted SSH signing key" >&2
    exit 1
  fi

  git_signing_pub="${git_signing_key}.pub"
  ssh-keygen -y -P "" -f "${git_signing_key}" > "${git_signing_pub}"
  printf '%s %s\n' "${git_user_email}" "$(cat "${git_signing_pub}")" > "${git_signing_allowed_signers}"
}

configure_git_release_identity_env() {
  load_git_release_identity

  export GIT_AUTHOR_NAME="${git_author_name}"
  export GIT_AUTHOR_EMAIL="${git_author_email}"
  export GIT_COMMITTER_NAME="${git_committer_name}"
  export GIT_COMMITTER_EMAIL="${git_committer_email}"
  export GIT_SIGNING_KEY_PATH="${git_signing_key}"
  export GIT_TERMINAL_PROMPT="0"

  # Apply SSH commit signing for any git process spawned by this script
  # (including GoReleaser's tap commit).
  export GIT_CONFIG_COUNT=10
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
  export GIT_CONFIG_KEY_9="gpg.ssh.allowedSignersFile"
  export GIT_CONFIG_VALUE_9="${git_signing_allowed_signers}"
}

run_cli() {
  resolve_tag
  load_publish_secrets
  configure_git_release_identity_env
  (
    cd "${repo_root}/hydra-go"
    goreleaser release --clean --config .goreleaser.yml
  )
}

render_homebrew_formula() {
  local formula_file="$1"
  local source_url="$2"
  local source_sha256="$3"
  local release_commit="$4"

  cat >"${formula_file}" <<EOF
# typed: false
# frozen_string_literal: true

# This file is generated by the Hydra publish workflow. DO NOT EDIT.
class Hydra < Formula
  desc "Hydra GitOps CLI for Kubernetes cluster management"
  homepage "https://hydra-gitops.org/"
  version "${version_core}"
  license "Apache-2.0"

  depends_on "go" => :build

  url "${source_url}"
  sha256 "${source_sha256}"

  def install
    cd "hydra-go" do
      system "go", "build", *std_go_args(ldflags: "-s -w -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.Version=#{version} -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.TagSHA=${release_commit}"), "./cli"
    end
  end

  test do
    assert_match "Hydra", shell_output("#{bin}/hydra --help")
  end
end
EOF
}

run_homebrew_formula() {
  resolve_tag
  load_publish_secrets
  configure_git_release_identity_env

  local release_repo source_archive_url source_archive_file source_sha256
  release_repo="${GITHUB_REPOSITORY:-${HYDRA_SECRETS_REPO:-}}"
  if [[ -z "${release_repo}" ]]; then
    echo "GITHUB_REPOSITORY or HYDRA_SECRETS_REPO must be set" >&2
    exit 1
  fi

  source_archive_url="https://github.com/${release_repo}/archive/refs/tags/${tag}.tar.gz"
  source_archive_file="${tmp_dir}/hydra_${tag}_source.tar.gz"
  curl -fsSL "${source_archive_url}" -o "${source_archive_file}"
  source_sha256="$(sha256_file "${source_archive_file}")"

  homebrew_formula_context="$(mktemp -d "${tmp_dir}/hydra-homebrew-formula.XXXXXX")"

  local tap_repo_url ssh_command
  tap_repo_url="ssh://git@github.com/${homebrew_tap_deploy_target_owner}/${homebrew_tap_deploy_target_name}.git"
  ssh_command="ssh -i ${homebrew_tap_deploy_key} -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -F /dev/null"

  GIT_SSH_COMMAND="${ssh_command}" git clone --depth 1 --branch main "${tap_repo_url}" "${homebrew_formula_context}"

  mkdir -p "${homebrew_formula_context}/Formula"
  render_homebrew_formula "${homebrew_formula_context}/Formula/hydra.rb" "${source_archive_url}" "${source_sha256}" "$(git -C "${repo_root}" rev-list -n1 "${tag}")"

  git -C "${homebrew_formula_context}" config user.name "${git_user_name}"
  git -C "${homebrew_formula_context}" config user.email "${git_user_email}"
  git -C "${homebrew_formula_context}" config gpg.format ssh
  git -C "${homebrew_formula_context}" config user.signingkey "${git_signing_key}"
  git -C "${homebrew_formula_context}" config commit.gpgsign true
  git -C "${homebrew_formula_context}" config gpg.ssh.allowedSignersFile "${git_signing_allowed_signers}"

  git -C "${homebrew_formula_context}" add Formula/hydra.rb
  if git -C "${homebrew_formula_context}" diff --cached --quiet; then
    echo "Homebrew formula already up to date for ${tag}" >&2
    return
  fi

  git -C "${homebrew_formula_context}" commit -S -m "chore: update hydra formula for ${tag}"
  GIT_SSH_COMMAND="${ssh_command}" git -C "${homebrew_formula_context}" push origin HEAD:main
}

prepare_container_context() {
  local amd64_binary arm64_binary
  amd64_binary="${repo_root}/dist/hydra_linux_amd64_v2/hydra"
  arm64_binary=""

  if [[ -f "${repo_root}/dist/hydra_linux_arm64/hydra" ]]; then
    arm64_binary="${repo_root}/dist/hydra_linux_arm64/hydra"
  elif [[ -f "${repo_root}/dist/hydra_linux_arm64_v8.0/hydra" ]]; then
    arm64_binary="${repo_root}/dist/hydra_linux_arm64_v8.0/hydra"
  fi

  if [[ ! -f "${amd64_binary}" || -z "${arm64_binary}" ]]; then
    echo "Expected goreleaser binaries at dist/hydra_linux_amd64_v2/hydra and dist/hydra_linux_arm64/hydra (or dist/hydra_linux_arm64_v8.0/hydra)" >&2
    exit 1
  fi

  container_context="$(mktemp -d "${tmp_dir}/hydra-container.XXXXXX")"
  mkdir -p "${container_context}/linux/amd64" "${container_context}/linux/arm64"

  cp "${amd64_binary}" "${container_context}/linux/amd64/hydra"
  cp "${arm64_binary}" "${container_context}/linux/arm64/hydra"
}

run_container() {
  resolve_tag
  load_publish_secrets

  if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_ACTOR:-}" || -z "${GITHUB_REPOSITORY:-}" ]]; then
    echo "GITHUB_TOKEN, GITHUB_ACTOR and GITHUB_REPOSITORY must be set" >&2
    exit 1
  fi

  prepare_container_context

  local image digest container_tags=() build_tags=()
  image="ghcr.io/${GITHUB_REPOSITORY,,}"
  mapfile -t container_tags < <(container_tags_for_release)

  echo "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

  docker buildx create --name hydra-release-builder --driver docker-container --use >/dev/null 2>&1 || docker buildx use hydra-release-builder
  docker buildx inspect --bootstrap >/dev/null

  for container_tag in "${container_tags[@]}"; do
    build_tags+=(--tag "${image}:${container_tag}")
  done

  docker buildx build \
    --platform linux/amd64,linux/arm64 \
    --file "${repo_root}/tools/build-container-image/Dockerfile" \
    --build-arg "VERSION=${tag}" \
    "${build_tags[@]}" \
    --push \
    "${container_context}"

  local inspect_output
  echo "Resolving digest for ${image}:${tag}"
  inspect_output="$(docker buildx imagetools inspect "${image}:${tag}" 2>&1)"

  digest="$(awk '/^Digest:/{print $2; exit}' <<<"${inspect_output}" | tr -d '[:space:]')"
  if [[ -z "${digest}" ]]; then
    echo "Could not resolve digest for ${image}:${tag}" >&2
    echo "imagetools inspect output:" >&2
    echo "${inspect_output}" >&2
    exit 1
  fi

  echo "Resolved digest: ${digest}"
  echo "Signing image ${image}@${digest}"

  cosign sign --yes --key "${cosign_key}" "${image}@${digest}"
}

usage() {
  cat <<'EOF'
Usage: scripts/publish.sh [verify|cli|formula|container|all]
EOF
}

subcommand="${1:-all}"
case "${subcommand}" in
  verify)
    verify
    ;;
  cli)
    run_cli
    ;;
  formula)
    run_homebrew_formula
    ;;
  container)
    run_container
    ;;
  all)
    verify
    run_cli
    run_homebrew_formula
    run_container
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
