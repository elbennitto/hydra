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
manual_publish_context=""
homebrew_tap_deploy_key=""
homebrew_tap_deploy_target_repo=""
homebrew_tap_deploy_target_owner=""
homebrew_tap_deploy_target_name=""
manual_pages_domain=""
manual_site_url=""
manual_pages_deploy_key=""
manual_pages_deploy_key_path=""
manual_pages_target_repo=""
manual_pages_target_dir=""
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
  rm -f "${cosign_key:-}" "${allowed_signers:-}" "${homebrew_tap_deploy_key:-}" "${manual_pages_deploy_key_path:-}"
  rm -f "${git_signing_key:-}" "${git_signing_pub:-}" "${git_signing_allowed_signers:-}"
  rm -rf "${container_context:-}" "${homebrew_formula_context:-}" "${manual_publish_context:-}"
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

extract_publish_field() {
  local file_path=""
  local section_name=""
  local field_name=""
  if [[ $# -eq 3 ]]; then
    file_path="$1"
    section_name="$2"
    field_name="$3"
  else
    file_path="${secrets_dir}/publish.yaml"
    section_name="$1"
    field_name="$2"
  fi

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

  homebrew_tap_deploy_target_repo="$(extract_publish_field "homebrew" "tap_deploy_target_repo")"

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

load_manual_publish_settings() {
  ensure_sops_key

  local release_public_config_file="${secrets_dir}/release.yaml"
  if [[ ! -f "${release_public_config_file}" ]]; then
    echo "Missing required release config: ${release_public_config_file}" >&2
    exit 1
  fi

  manual_pages_domain="${HYDRA_MANUAL_PAGES_DOMAIN:-}"
  if [[ -z "${manual_pages_domain}" ]]; then
    manual_pages_domain="$(extract_publish_field "${release_public_config_file}" "manual" "pages_domain")"
  fi
  manual_pages_domain="${manual_pages_domain:-docs.hydra-gitops.org}"

  manual_site_url="${HYDRA_DOCS_SITE_URL:-}"
  if [[ -z "${manual_site_url}" ]]; then
    manual_site_url="$(extract_publish_field "${release_public_config_file}" "manual" "site_url")"
  fi
  if [[ -z "${manual_site_url}" ]]; then
    manual_site_url="https://${manual_pages_domain}/"
  fi

  manual_pages_target_repo="${HYDRA_MANUAL_PAGES_TARGET_REPO:-}"
  if [[ -z "${manual_pages_target_repo}" ]]; then
    manual_pages_target_repo="$(extract_publish_field "${release_public_config_file}" "manual" "target_repo")"
  fi
  if [[ -z "${manual_pages_target_repo}" ]]; then
    manual_pages_target_repo="${GITHUB_REPOSITORY:-${HYDRA_SECRETS_REPO:-}}"
  fi

  manual_pages_target_dir="${HYDRA_MANUAL_PAGES_TARGET_DIR:-}"
  if [[ -z "${manual_pages_target_dir}" ]]; then
    manual_pages_target_dir="$(extract_publish_field "${release_public_config_file}" "manual" "target_dir")"
  fi
  manual_pages_target_dir="${manual_pages_target_dir:-.}"
  manual_pages_target_dir="${manual_pages_target_dir#/}"
  manual_pages_target_dir="${manual_pages_target_dir%/}"
  manual_pages_target_dir="${manual_pages_target_dir:-.}"

  manual_pages_deploy_key="${MANUAL_PAGES_DEPLOY_KEY:-}"
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

run_cli_target() {
  resolve_tag

  local target_goos target_goarch target_goamd64 release_commit output_suffix output_dir
  target_goos="${TARGET_GOOS:-}"
  target_goarch="${TARGET_GOARCH:-}"
  target_goamd64="${TARGET_GOAMD64:-}"

  if [[ -z "${target_goos}" || -z "${target_goarch}" ]]; then
    echo "TARGET_GOOS and TARGET_GOARCH must be set" >&2
    exit 1
  fi

  output_suffix=""
  if [[ -n "${target_goamd64}" ]]; then
    output_suffix="_${target_goamd64}"
  fi

  output_dir="${repo_root}/dist/hydra_${target_goos}_${target_goarch}${output_suffix}"
  release_commit="$(git -C "${repo_root}" rev-list -n1 "${tag}")"

  mkdir -p "${output_dir}"

  (
    cd "${repo_root}/hydra-go"
    GOOS="${target_goos}" \
    GOARCH="${target_goarch}" \
    GOAMD64="${target_goamd64}" \
    CGO_ENABLED=0 \
      go build \
        -trimpath \
        -ldflags "-s -w -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.Version=${version_core} -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.TagSHA=${release_commit}" \
        -o "${output_dir}/hydra" \
        ./cli
  )
}

target_output_suffix() {
  local target_goamd64="$1"

  if [[ -n "${target_goamd64}" ]]; then
    printf '_%s' "${target_goamd64}"
  fi
}

target_archive_name() {
  local target_goos="$1"
  local target_goarch="$2"

  printf 'hydra_%s_%s_%s.tar.gz' "${version_core}" "${target_goos}" "${target_goarch}"
}

package_cli_target() {
  resolve_tag

  local target_goos target_goarch target_goamd64 output_suffix binary_path archive_name assets_dir archive_path checksum_path
  target_goos="${TARGET_GOOS:-}"
  target_goarch="${TARGET_GOARCH:-}"
  target_goamd64="${TARGET_GOAMD64:-}"

  if [[ -z "${target_goos}" || -z "${target_goarch}" ]]; then
    echo "TARGET_GOOS and TARGET_GOARCH must be set" >&2
    exit 1
  fi

  output_suffix="$(target_output_suffix "${target_goamd64}")"
  binary_path="${repo_root}/dist/hydra_${target_goos}_${target_goarch}${output_suffix}/hydra"
  if [[ ! -f "${binary_path}" ]]; then
    echo "Expected target binary not found: ${binary_path}" >&2
    exit 1
  fi

  archive_name="$(target_archive_name "${target_goos}" "${target_goarch}")"
  assets_dir="${repo_root}/dist/release_assets"
  archive_path="${assets_dir}/${archive_name}"
  checksum_path="${assets_dir}/checksums/${archive_name}.sha256"

  mkdir -p "${assets_dir}/checksums"
  tar -C "$(dirname "${binary_path}")" -czf "${archive_path}" hydra
  printf '%s  %s\n' "$(sha256_file "${archive_path}")" "${archive_name}" > "${checksum_path}"

  printf '%s\n' "${archive_path}"
}

ensure_github_release() {
  resolve_tag

  if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    echo "GITHUB_TOKEN must be set" >&2
    exit 1
  fi

  if gh release view "${tag}" >/dev/null 2>&1; then
    return
  fi

  if gh release create "${tag}" --verify-tag --title "${tag}" --generate-notes; then
    return
  fi

  gh release view "${tag}" >/dev/null
}

run_cli_target_publish() {
  local archive_path

  resolve_tag
  load_publish_secrets
  run_cli_target
  archive_path="$(package_cli_target)"

  cosign sign-blob --yes --key "${cosign_key}" --bundle="${archive_path}.sigstore.json" "${archive_path}"
  ensure_github_release
  gh release upload "${tag}" "${archive_path}" "${archive_path}.sigstore.json" --clobber
}

run_cli_checksums() {
  resolve_tag
  load_publish_secrets

  local assets_dir checksums_file checksum_bundle checksum_fragments=()
  assets_dir="${repo_root}/dist/release_assets"
  checksums_file="${assets_dir}/checksums.txt"
  checksum_bundle="${checksums_file}.sigstore.json"

  if [[ -d "${assets_dir}/checksums" ]]; then
    while IFS= read -r checksum_fragment; do
      checksum_fragments+=("${checksum_fragment}")
    done < <(find "${assets_dir}/checksums" -type f -name '*.sha256' | sort)
  fi

  if [[ ${#checksum_fragments[@]} -eq 0 ]]; then
    echo "No CLI checksum fragments found under ${assets_dir}/checksums" >&2
    exit 1
  fi

  mkdir -p "${assets_dir}"
  cat "${checksum_fragments[@]}" > "${checksums_file}"
  cosign sign-blob --yes --key "${cosign_key}" --bundle="${checksum_bundle}" "${checksums_file}"

  ensure_github_release
  gh release upload "${tag}" "${checksums_file}" "${checksum_bundle}" --clobber
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

release_archive_path() {
  local target_goos="$1"
  local target_goarch="$2"
  local archive_name

  archive_name="$(target_archive_name "${target_goos}" "${target_goarch}")"
  printf '%s/%s' "${repo_root}/dist/release_assets" "${archive_name}"
}

release_archive_sha256() {
  local target_goos="$1"
  local target_goarch="$2"
  local archive_name archive_path

  archive_name="$(target_archive_name "${target_goos}" "${target_goarch}")"
  archive_path="$(release_archive_path "${target_goos}" "${target_goarch}")"
  if [[ ! -f "${archive_path}" ]]; then
    echo "Required release artifact is missing: ${archive_path}" >&2
    echo "Ensure workflow artifacts were downloaded and extracted before running homebrew publish." >&2
    exit 1
  fi

  sha256_file "${archive_path}"
}

download_source_archive() {
  local archive_url="$1"
  local archive_file="$2"
  local gh_token=""

  if command -v gh >/dev/null 2>&1; then
    gh_token="$(gh auth token 2>/dev/null || true)"
  fi

  if [[ -n "${gh_token}" ]] && curl -fsSL -H "Authorization: token ${gh_token}" "${archive_url}" -o "${archive_file}"; then
    return
  fi

  if [[ -n "${GITHUB_TOKEN:-}" ]] && curl -fsSL -H "Authorization: token ${GITHUB_TOKEN}" "${archive_url}" -o "${archive_file}"; then
    return
  fi

  if [[ -n "${GH_TOKEN:-}" ]] && curl -fsSL -H "Authorization: token ${GH_TOKEN}" "${archive_url}" -o "${archive_file}"; then
    return
  fi

  curl -fsSL "${archive_url}" -o "${archive_file}"
}

render_homebrew_cask() {
  local cask_file="$1"
  local release_repo="$2"
  local linux_amd64_sha256="$3"
  local linux_arm64_sha256="$4"
  local darwin_amd64_sha256="$5"
  local darwin_arm64_sha256="$6"
  local release_base="https://github.com/${release_repo}/releases/download/${tag}"

  cat >"${cask_file}" <<EOF
# typed: false
# frozen_string_literal: true

# This file is generated by the Hydra publish workflow. DO NOT EDIT.
cask "hydra-bin" do
  version "${version_core}"
  name "Hydra"
  desc "Hydra GitOps CLI binary for Kubernetes cluster management"
  homepage "https://hydra-gitops.org/"

  on_macos do
    if Hardware::CPU.arm?
      url "${release_base}/hydra_#{version}_darwin_arm64.tar.gz"
      sha256 "${darwin_arm64_sha256}"
    else
      url "${release_base}/hydra_#{version}_darwin_amd64.tar.gz"
      sha256 "${darwin_amd64_sha256}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${release_base}/hydra_#{version}_linux_arm64.tar.gz"
      sha256 "${linux_arm64_sha256}"
    else
      url "${release_base}/hydra_#{version}_linux_amd64.tar.gz"
      sha256 "${linux_amd64_sha256}"
    end
  end

  binary "hydra"
end
EOF
}

run_homebrew_formula() {
  resolve_tag
  load_publish_secrets
  configure_git_release_identity_env

  local release_repo source_archive_url source_archive_download_url source_archive_file source_sha256 linux_amd64_sha256 linux_arm64_sha256 darwin_amd64_sha256 darwin_arm64_sha256
  release_repo="${GITHUB_REPOSITORY:-${HYDRA_SECRETS_REPO:-}}"
  if [[ -z "${release_repo}" ]]; then
    echo "GITHUB_REPOSITORY or HYDRA_SECRETS_REPO must be set" >&2
    exit 1
  fi

  source_archive_url="https://github.com/${release_repo}/archive/refs/tags/${tag}.tar.gz"
  source_archive_download_url="https://codeload.github.com/${release_repo}/tar.gz/refs/tags/${tag}"
  source_archive_file="${tmp_dir}/hydra_${tag}_source.tar.gz"
  download_source_archive "${source_archive_download_url}" "${source_archive_file}"
  source_sha256="$(sha256_file "${source_archive_file}")"

  linux_amd64_sha256="$(release_archive_sha256 linux amd64)"
  linux_arm64_sha256="$(release_archive_sha256 linux arm64)"
  darwin_amd64_sha256="$(release_archive_sha256 darwin amd64)"
  darwin_arm64_sha256="$(release_archive_sha256 darwin arm64)"

  homebrew_formula_context="$(mktemp -d "${tmp_dir}/hydra-homebrew-formula.XXXXXX")"

  local tap_repo_url ssh_command
  tap_repo_url="ssh://git@github.com/${homebrew_tap_deploy_target_owner}/${homebrew_tap_deploy_target_name}.git"
  ssh_command="ssh -i ${homebrew_tap_deploy_key} -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -F /dev/null"

  GIT_SSH_COMMAND="${ssh_command}" git clone --depth 1 --branch main "${tap_repo_url}" "${homebrew_formula_context}"

  mkdir -p "${homebrew_formula_context}/Formula"
  render_homebrew_formula "${homebrew_formula_context}/Formula/hydra.rb" "${source_archive_url}" "${source_sha256}" "$(git -C "${repo_root}" rev-list -n1 "${tag}")"
  mkdir -p "${homebrew_formula_context}/Casks"
  render_homebrew_cask "${homebrew_formula_context}/Casks/hydra-bin.rb" "${release_repo}" "${linux_amd64_sha256}" "${linux_arm64_sha256}" "${darwin_amd64_sha256}" "${darwin_arm64_sha256}"

  git -C "${homebrew_formula_context}" config user.name "${git_user_name}"
  git -C "${homebrew_formula_context}" config user.email "${git_user_email}"
  git -C "${homebrew_formula_context}" config gpg.format ssh
  git -C "${homebrew_formula_context}" config user.signingkey "${git_signing_key}"
  git -C "${homebrew_formula_context}" config commit.gpgsign true
  git -C "${homebrew_formula_context}" config gpg.ssh.allowedSignersFile "${git_signing_allowed_signers}"

  git -C "${homebrew_formula_context}" add Formula/hydra.rb Casks/hydra-bin.rb
  if git -C "${homebrew_formula_context}" diff --cached --quiet; then
    echo "Homebrew formulas already up to date for ${tag}" >&2
    return
  fi

  git -C "${homebrew_formula_context}" commit -S -m "chore: update hydra homebrew packages for ${tag}"
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

publish_container_image() {
  local image="$1"
  local dockerfile_path="$2"
  local digest inspect_output
  local container_tags=()
  local build_tags=()
  mapfile -t container_tags < <(container_tags_for_release)
  for container_tag in "${container_tags[@]}"; do
    build_tags+=(--tag "${image}:${container_tag}")
  done

  docker buildx build \
    --platform linux/amd64,linux/arm64 \
    --file "${dockerfile_path}" \
    --build-arg "VERSION=${tag}" \
    "${build_tags[@]}" \
    --push \
    "${container_context}"

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

run_container() {
  local repo_owner repo_name runtime_image ci_image

  resolve_tag
  load_publish_secrets

  if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_ACTOR:-}" || -z "${GITHUB_REPOSITORY:-}" ]]; then
    echo "GITHUB_TOKEN, GITHUB_ACTOR and GITHUB_REPOSITORY must be set" >&2
    exit 1
  fi

  prepare_container_context

  repo_owner="${GITHUB_REPOSITORY%%/*}"
  repo_name="${GITHUB_REPOSITORY##*/}"
  runtime_image="ghcr.io/${GITHUB_REPOSITORY,,}"
  ci_image="ghcr.io/${repo_owner,,}/${repo_name,,}-ci"

  echo "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

  docker buildx create --name hydra-release-builder --driver docker-container --use >/dev/null 2>&1 || docker buildx use hydra-release-builder
  docker buildx inspect --bootstrap >/dev/null

  publish_container_image "${runtime_image}" "${repo_root}/tools/build-container-image/Dockerfile"
  publish_container_image "${ci_image}" "${repo_root}/tools/build-container-image/Dockerfile.ci"
}

container_tags_for_target_release() {
  resolve_tag

  local target_arch="$1"
  local tags=("${tag}-linux-${target_arch}")
  if [[ "${tag}" == "v${version_core}" ]]; then
    tags+=("v${version_major}.${version_minor}-linux-${target_arch}" "v${version_major}-linux-${target_arch}" "latest-linux-${target_arch}")
  fi

  printf '%s\n' "${tags[@]}"
}

publish_container_image_for_target() {
  local image="$1"
  local dockerfile_path="$2"
  local target_arch="$3"
  local digest inspect_output
  local container_tags=()
  local build_tags=()

  mapfile -t container_tags < <(container_tags_for_target_release "${target_arch}")
  for container_tag in "${container_tags[@]}"; do
    build_tags+=(--tag "${image}:${container_tag}")
  done

  docker buildx build \
    --platform "linux/${target_arch}" \
    --file "${dockerfile_path}" \
    --build-arg "VERSION=${tag}" \
    "${build_tags[@]}" \
    --push \
    "${container_context}"

  for container_tag in "${container_tags[@]}"; do
    echo "Resolving digest for ${image}:${container_tag}"
    inspect_output="$(docker buildx imagetools inspect "${image}:${container_tag}" 2>&1)"

    digest="$(awk '/^Digest:/{print $2; exit}' <<<"${inspect_output}" | tr -d '[:space:]')"
    if [[ -z "${digest}" ]]; then
      echo "Could not resolve digest for ${image}:${container_tag}" >&2
      echo "imagetools inspect output:" >&2
      echo "${inspect_output}" >&2
      exit 1
    fi

    echo "Resolved digest: ${digest}"
    echo "Signing image ${image}@${digest}"
    cosign sign --yes --key "${cosign_key}" "${image}@${digest}"
  done
}

publish_container_manifests() {
  local image="$1"
  local digest inspect_output
  local container_tags=()

  mapfile -t container_tags < <(container_tags_for_release)
  for container_tag in "${container_tags[@]}"; do
    echo "Creating multi-arch manifest for ${image}:${container_tag}"
    docker buildx imagetools create \
      --tag "${image}:${container_tag}" \
      "${image}:${container_tag}-linux-amd64" \
      "${image}:${container_tag}-linux-arm64"

    echo "Resolving digest for ${image}:${container_tag}"
    inspect_output="$(docker buildx imagetools inspect "${image}:${container_tag}" 2>&1)"

    digest="$(awk '/^Digest:/{print $2; exit}' <<<"${inspect_output}" | tr -d '[:space:]')"
    if [[ -z "${digest}" ]]; then
      echo "Could not resolve digest for ${image}:${container_tag}" >&2
      echo "imagetools inspect output:" >&2
      echo "${inspect_output}" >&2
      exit 1
    fi

    echo "Resolved digest: ${digest}"
    echo "Signing image ${image}@${digest}"
    cosign sign --yes --key "${cosign_key}" "${image}@${digest}"
  done
}

run_container_target() {
  local repo_owner repo_name runtime_image ci_image target_goarch target_goamd64 binary_suffix binary_path

  resolve_tag
  load_publish_secrets

  if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_ACTOR:-}" || -z "${GITHUB_REPOSITORY:-}" ]]; then
    echo "GITHUB_TOKEN, GITHUB_ACTOR and GITHUB_REPOSITORY must be set" >&2
    exit 1
  fi

  target_goarch="${TARGET_GOARCH:-}"
  target_goamd64="${TARGET_GOAMD64:-}"
  if [[ -z "${target_goarch}" ]]; then
    echo "TARGET_GOARCH must be set" >&2
    exit 1
  fi

  binary_suffix=""
  if [[ -n "${target_goamd64}" ]]; then
    binary_suffix="_${target_goamd64}"
  fi

  binary_path="${repo_root}/dist/hydra_linux_${target_goarch}${binary_suffix}/hydra"
  if [[ ! -f "${binary_path}" ]]; then
    echo "Expected target binary not found: ${binary_path}" >&2
    exit 1
  fi

  container_context="$(mktemp -d "${tmp_dir}/hydra-container-target.XXXXXX")"
  mkdir -p "${container_context}/linux/${target_goarch}"
  cp "${binary_path}" "${container_context}/linux/${target_goarch}/hydra"

  repo_owner="${GITHUB_REPOSITORY%%/*}"
  repo_name="${GITHUB_REPOSITORY##*/}"
  runtime_image="ghcr.io/${GITHUB_REPOSITORY,,}"
  ci_image="ghcr.io/${repo_owner,,}/${repo_name,,}-ci"

  echo "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

  docker buildx create --name hydra-release-builder --driver docker-container --use >/dev/null 2>&1 || docker buildx use hydra-release-builder
  docker buildx inspect --bootstrap >/dev/null

  publish_container_image_for_target "${runtime_image}" "${repo_root}/tools/build-container-image/Dockerfile" "${target_goarch}"
  publish_container_image_for_target "${ci_image}" "${repo_root}/tools/build-container-image/Dockerfile.ci" "${target_goarch}"
}

run_container_merge() {
  local repo_owner repo_name runtime_image ci_image

  resolve_tag
  load_publish_secrets

  if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_ACTOR:-}" || -z "${GITHUB_REPOSITORY:-}" ]]; then
    echo "GITHUB_TOKEN, GITHUB_ACTOR and GITHUB_REPOSITORY must be set" >&2
    exit 1
  fi

  repo_owner="${GITHUB_REPOSITORY%%/*}"
  repo_name="${GITHUB_REPOSITORY##*/}"
  runtime_image="ghcr.io/${GITHUB_REPOSITORY,,}"
  ci_image="ghcr.io/${repo_owner,,}/${repo_name,,}-ci"

  echo "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

  docker buildx create --name hydra-release-builder --driver docker-container --use >/dev/null 2>&1 || docker buildx use hydra-release-builder
  docker buildx inspect --bootstrap >/dev/null

  publish_container_manifests "${runtime_image}"
  publish_container_manifests "${ci_image}"
}

run_manual() {
  load_manual_publish_settings
  configure_git_release_identity_env

  local release_repo source_dir target_dir deploy_remote deploy_path
  local deploy_ssh_command="" auth_header="" use_https_token="false"
  release_repo="${GITHUB_REPOSITORY:-${HYDRA_SECRETS_REPO:-}}"
  if [[ -z "${release_repo}" ]]; then
    echo "GITHUB_REPOSITORY or HYDRA_SECRETS_REPO must be set" >&2
    exit 1
  fi

  source_dir="${repo_root}/docs/site/site"

  (
    cd "${repo_root}/docs/site"
    ./build.sh --site-url "${manual_site_url}"
  )

  printf '%s\n' "${manual_pages_domain}" > "${source_dir}/CNAME"
  touch "${source_dir}/.nojekyll"

  target_dir="$(mktemp -d "${tmp_dir}/hydra-manual-pages.XXXXXX")"
  manual_publish_context="${target_dir}"

  if [[ "${manual_pages_target_repo}" == "${release_repo}" && -n "${GITHUB_TOKEN:-}" ]]; then
    deploy_remote="https://github.com/${manual_pages_target_repo}.git"
    auth_header="$(printf 'x-access-token:%s' "${GITHUB_TOKEN}" | base64 | tr -d '\n')"
    use_https_token="true"
  else
    if [[ -z "${manual_pages_deploy_key}" ]]; then
      manual_pages_deploy_key="$(sops --decrypt --extract '["manual"]["pages_deploy_key"]' "${secrets_dir}/publish.sops.yaml" 2>/dev/null || true)"
    fi
    if [[ -z "${manual_pages_deploy_key}" || "${manual_pages_deploy_key}" == "null" ]]; then
      echo "Could not load manual deploy key from publish secrets (expected publish.sops.yaml: manual.pages_deploy_key)" >&2
      exit 1
    fi

    manual_pages_deploy_key_path="${tmp_dir}/manual_pages_deploy_key"
    if [[ "${manual_pages_deploy_key}" == *\\n* ]]; then
      printf '%b\n' "${manual_pages_deploy_key}" > "${manual_pages_deploy_key_path}"
    else
      printf '%s\n' "${manual_pages_deploy_key}" > "${manual_pages_deploy_key_path}"
    fi
    chmod 600 "${manual_pages_deploy_key_path}"

    deploy_remote="ssh://git@github.com/${manual_pages_target_repo}.git"
    deploy_ssh_command="ssh -i ${manual_pages_deploy_key_path} -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -F /dev/null"
  fi

  if [[ "${use_https_token}" == "true" ]]; then
    if git -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth_header}" ls-remote --exit-code --heads "${deploy_remote}" gh-pages >/dev/null 2>&1; then
      git -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth_header}" clone --depth 1 --branch gh-pages "${deploy_remote}" "${target_dir}"
    else
      git init "${target_dir}"
      git -C "${target_dir}" remote add origin "${deploy_remote}"
    fi
  elif GIT_SSH_COMMAND="${deploy_ssh_command}" git ls-remote --exit-code --heads "${deploy_remote}" gh-pages >/dev/null 2>&1; then
    GIT_SSH_COMMAND="${deploy_ssh_command}" git clone --depth 1 --branch gh-pages "${deploy_remote}" "${target_dir}"
  else
    git init "${target_dir}"
    git -C "${target_dir}" remote add origin "${deploy_remote}"
  fi

  git -C "${target_dir}" config user.name "${git_user_name}"
  git -C "${target_dir}" config user.email "${git_user_email}"
  git -C "${target_dir}" config gpg.format ssh
  git -C "${target_dir}" config user.signingkey "${git_signing_key}"
  git -C "${target_dir}" config commit.gpgsign true
  git -C "${target_dir}" config gpg.ssh.allowedSignersFile "${git_signing_allowed_signers}"

  local source_revision
  source_revision="${GITHUB_SHA:-$(git -C "${repo_root}" rev-parse HEAD)}"

  local max_push_attempts push_attempt branch_exists="false"
  max_push_attempts=3

  for ((push_attempt = 1; push_attempt <= max_push_attempts; push_attempt++)); do
    if [[ "${use_https_token}" == "true" ]]; then
      if git -C "${target_dir}" -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth_header}" fetch --depth 1 origin gh-pages:refs/remotes/origin/gh-pages >/dev/null 2>&1; then
        branch_exists="true"
      else
        branch_exists="false"
      fi
    elif GIT_SSH_COMMAND="${deploy_ssh_command}" git -C "${target_dir}" fetch --depth 1 origin gh-pages:refs/remotes/origin/gh-pages >/dev/null 2>&1; then
      branch_exists="true"
    else
      branch_exists="false"
    fi

    if [[ "${branch_exists}" == "true" ]]; then
      if git -C "${target_dir}" show-ref --verify --quiet refs/heads/gh-pages; then
        git -C "${target_dir}" checkout gh-pages
      else
        git -C "${target_dir}" checkout -B gh-pages refs/remotes/origin/gh-pages
      fi
      git -C "${target_dir}" reset --hard refs/remotes/origin/gh-pages
    elif git -C "${target_dir}" rev-parse --verify main >/dev/null 2>&1; then
      git -C "${target_dir}" checkout -B gh-pages
      find "${target_dir}" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
    else
      git -C "${target_dir}" checkout --orphan gh-pages
      find "${target_dir}" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
    fi

    if [[ "${manual_pages_target_dir}" == "." ]]; then
      find "${target_dir}" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
      cp -a "${source_dir}/." "${target_dir}/"
    else
      deploy_path="${target_dir}/${manual_pages_target_dir}"
      rm -rf "${deploy_path}"
      mkdir -p "${deploy_path}"
      cp -a "${source_dir}/." "${deploy_path}/"
    fi

    git -C "${target_dir}" add --all
    if git -C "${target_dir}" diff --cached --quiet; then
      echo "Manual site already up to date; nothing to publish"
      return
    fi

    git -C "${target_dir}" commit -S -m "docs: publish manual from ${source_revision}"

    if [[ "${use_https_token}" == "true" ]]; then
      if git -C "${target_dir}" -c "http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth_header}" push origin HEAD:gh-pages; then
        return
      fi
    elif GIT_SSH_COMMAND="${deploy_ssh_command}" git -C "${target_dir}" push origin HEAD:gh-pages; then
      return
    fi

    if (( push_attempt < max_push_attempts )); then
      echo "Push to gh-pages was rejected; refreshing remote branch and retrying (${push_attempt}/${max_push_attempts})"
    fi
  done

  echo "Failed to publish manual after ${max_push_attempts} attempts because gh-pages kept changing" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: scripts/publish.sh [verify|cli|cli-target|cli-target-publish|cli-checksums|formula|homebrew|container|container-target|container-merge|manual|all]
EOF
}

subcommand="${1:-all}"
case "${subcommand}" in
  verify)
    verify
    ;;
  cli)
    if [[ -n "${TARGET_GOOS:-}" || -n "${TARGET_GOARCH:-}" ]]; then
      run_cli_target
    else
      run_cli
    fi
    ;;
  cli-target)
    run_cli_target
    ;;
  cli-target-publish)
    run_cli_target_publish
    ;;
  cli-checksums)
    run_cli_checksums
    ;;
  formula)
    run_homebrew_formula
    ;;
  homebrew)
    run_homebrew_formula
    ;;
  container)
    run_container
    ;;
  container-target)
    run_container_target
    ;;
  container-merge)
    run_container_merge
    ;;
  manual)
    run_manual
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
