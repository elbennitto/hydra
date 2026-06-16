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
cosign_key=""
allowed_signers=""
container_context=""
homebrew_tap_deploy_key=""

cleanup() {
  rm -f "${cosign_key:-}" "${allowed_signers:-}" "${homebrew_tap_deploy_key:-}"
  rm -rf "${container_context:-}"
}
trap cleanup EXIT

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
}

ensure_sops_key() {
  : "${SOPS_AGE_KEY_PUBLISH:=${SOPS_AGE_KEY:-}}"
  if [[ -z "${SOPS_AGE_KEY_PUBLISH}" ]]; then
    SOPS_AGE_KEY_PUBLISH="$(sops --decrypt --extract '["age_keys"]["publish"]["private_key"]' "${secrets_dir}/age-pipeline-keys.sops.yaml" 2>/dev/null | grep AGE-SECRET-KEY-)"
  fi
  if [[ -z "${SOPS_AGE_KEY_PUBLISH}" ]]; then
    echo "SOPS_AGE_KEY_PUBLISH or SOPS_AGE_KEY must be set" >&2
    exit 1
  fi

  export SOPS_AGE_KEY="${SOPS_AGE_KEY_PUBLISH}"
}

verify() {
  resolve_tag
  ensure_sops_key

  mkdir -p "${tmp_dir}"

  local pub_openssh pub_key_material tag_commit tagger_name tagger_email
  pub_openssh="$(awk -F": " '/public_key_openssh:/{gsub(/^"|"$/, "", $2); print $2}' "${secrets_dir}/public-keys.yaml")"
  if [[ -z "${pub_openssh}" ]]; then
    echo "Could not read git_signing.public_key_openssh from ${secrets_dir}/public-keys.yaml" >&2
    exit 1
  fi

  pub_key_material="$(echo "${pub_openssh}" | awk '{print $1" "$2}')"
  allowed_signers="${tmp_dir}/allowed_signers"
  printf "%s %s\n" "release@hydra-gitops.org" "${pub_key_material}" > "${allowed_signers}"

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
  if [[ "${tagger_name}" != "Hydra Release" || "${tagger_email}" != "release@hydra-gitops.org" ]]; then
    echo "Tag ${tag} was not created by the expected release identity" >&2
    echo "Found tagger name='${tagger_name}' email='${tagger_email}'" >&2
    exit 1
  fi
}

load_publish_secrets() {
  ensure_sops_key

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

run_cli() {
  resolve_tag
  load_publish_secrets
  (
    cd "${repo_root}/hydra-go"
    goreleaser release --clean --config .goreleaser.yml
  )
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

  local image digest
  image="ghcr.io/${GITHUB_REPOSITORY,,}"

  echo "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

  docker buildx create --name hydra-release-builder --driver docker-container --use >/dev/null 2>&1 || docker buildx use hydra-release-builder
  docker buildx inspect --bootstrap >/dev/null

  docker buildx build \
    --platform linux/amd64,linux/arm64 \
    --file "${repo_root}/tools/build-container-image/Dockerfile" \
    --build-arg "VERSION=${tag}" \
    --tag "${image}:${tag}" \
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
Usage: scripts/publish.sh [verify|cli|container|all]
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
  container)
    run_container
    ;;
  all)
    verify
    run_cli
    run_container
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
