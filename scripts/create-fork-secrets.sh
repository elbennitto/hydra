#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/create-fork-secrets.sh

Example:
  scripts/create-fork-secrets.sh
EOF
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
secrets_root="${repo_root}/.github/secrets"
sops_config="${repo_root}/.sops.yaml"
release_sender_regex='^(.+)[[:space:]]<([^<>[:space:]]+@[^<>[:space:]]+)>$'

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -ne 0 ]]; then
  echo "This script does not accept positional parameters anymore." >&2
  usage >&2
  exit 1
fi

hydra_bin=""
fork_path=""
fork_public_key=""
release_sender_input=""
release_sender_name=""
release_sender_email=""
fork_owner=""
fork_repo=""
homebrew_tap_target_repo=""
manual_pages_domain=""
manual_site_url=""
manual_target_repo=""
manual_target_dir=""
dst_dir=""
fork_path_regex=""
fork_public_key_default=""
release_sender_input_default=""

existing_owner_public_key=""
existing_release_age_public=""
existing_publish_age_public=""
existing_git_signing_public_key=""
existing_cosign_public_key_pem=""
existing_homebrew_tap_public_key=""
existing_manual_pages_deploy_public_key=""
existing_homebrew_tap_target_repo=""
existing_cosign_private_key_pem=""
existing_cosign_password=""
existing_homebrew_tap_private_key=""
existing_manual_pages_deploy_private_key=""
existing_manual_pages_domain=""
existing_manual_site_url=""
existing_manual_target_repo=""
existing_manual_target_dir=""

recreate_age_bundle="true"
recreate_git_signing="true"
recreate_publish_bundle="true"
recreate_publish_cosign="true"
recreate_publish_homebrew_tap="true"
recreate_publish_manual_pages_deploy="true"
manage_manual_pages_deploy_key="false"

age_pipeline_file=""
git_secrets_file=""
publish_secrets_file=""
renovate_secrets_file=""

detect_default_fork_path() {
  local -a paths

  if [[ ! -d "${secrets_root}/repos" ]]; then
    return
  fi

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
  fi
}

extract_yaml_quoted_value() {
  local file="$1"
  local key="$2"

  sed -n -E "s/^${key}:[[:space:]]*\"(.*)\"[[:space:]]*$/\1/p" "${file}" | head -n1
}

extract_yaml_section_scalar() {
  local file="$1"
  local section="$2"
  local key="$3"

  awk -v section="${section}" -v key="${key}" '
    $0 ~ "^" section ":[[:space:]]*$" {
      in_section = 1
      next
    }

    in_section && $0 ~ "^[^[:space:]].*:[[:space:]]*$" {
      in_section = 0
    }

    in_section && $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
      value = $0
      sub("^[[:space:]]*" key ":[[:space:]]*", "", value)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if (value ~ /^".*"$/) {
        sub(/^"/, "", value)
        sub(/"$/, "", value)
      }
      print value
      exit
    }
  ' "${file}"
}

extract_yaml_section_block() {
  local file="$1"
  local section="$2"
  local key="$3"

  awk -v section="${section}" -v key="${key}" '
    $0 ~ "^" section ":[[:space:]]*$" {
      in_section = 1
      next
    }

    in_section && $0 ~ "^[^[:space:]].*:[[:space:]]*$" {
      in_section = 0
    }

    in_section && $0 ~ "^[[:space:]]*" key ":[[:space:]]*\\|[-+]?[[:space:]]*$" {
      in_block = 1
      next
    }

    in_block {
      if ($0 ~ "^    ") {
        value = $0
        sub(/^    /, "", value)
        print value
        next
      }
      exit
    }
  ' "${file}"
}

prompt_yes_no() {
  local label="$1"
  local default_answer="$2"
  local reply
  local normalized

  while true; do
    if [[ "${default_answer}" == "yes" ]]; then
      read -r -p "${label} [Y/n]: " reply
      if [[ -z "${reply}" ]]; then
        return 0
      fi
    else
      read -r -p "${label} [y/N]: " reply
      if [[ -z "${reply}" ]]; then
        return 1
      fi
    fi

    normalized="$(printf '%s' "${reply}" | tr '[:upper:]' '[:lower:]')"
    case "${normalized}" in
      y|yes)
        return 0
        ;;
      n|no)
        return 1
        ;;
      *)
        echo "Please answer yes or no." >&2
        ;;
    esac
  done
}

prompt_with_default() {
  local label="$1"
  local default_value="$2"
  local reply

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

load_existing_defaults_for_fork() {
  local selected_fork_path="$1"
  local selected_dir="${secrets_root}/repos/${selected_fork_path}"
  local existing_name=""
  local existing_email=""

  fork_public_key_default=""
  release_sender_input_default=""
  existing_owner_public_key=""
  existing_release_age_public=""
  existing_publish_age_public=""
  existing_git_signing_public_key=""
  existing_cosign_public_key_pem=""
  existing_homebrew_tap_public_key=""
  existing_manual_pages_deploy_public_key=""
  existing_homebrew_tap_target_repo=""
  existing_cosign_private_key_pem=""
  existing_cosign_password=""
  existing_homebrew_tap_private_key=""
  existing_manual_pages_deploy_private_key=""
  existing_manual_pages_domain=""
  existing_manual_site_url=""
  existing_manual_target_repo=""
  existing_manual_target_dir=""

  if [[ -f "${selected_dir}/public-keys.yaml" ]]; then
    fork_public_key_default="$(extract_yaml_quoted_value "${selected_dir}/public-keys.yaml" "owner_ssh_public_key")"
    existing_owner_public_key="${fork_public_key_default}"
    existing_release_age_public="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "semantic_release_age" "public_key")"
    existing_publish_age_public="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "publish_age" "public_key")"
    existing_git_signing_public_key="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "git_signing" "public_key_openssh")"
    existing_cosign_public_key_pem="$(extract_yaml_section_block "${selected_dir}/public-keys.yaml" "cosign" "public_key_pem")"
    existing_homebrew_tap_public_key="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "homebrew_tap" "public_key_openssh")"
    existing_manual_pages_deploy_public_key="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "manual_pages" "public_key_openssh")"
  fi

  if [[ -f "${selected_dir}/git.yaml" ]]; then
    existing_name="$(extract_yaml_quoted_value "${selected_dir}/git.yaml" "  name")"
    existing_email="$(extract_yaml_quoted_value "${selected_dir}/git.yaml" "  email")"
    if [[ -n "${existing_name}" && -n "${existing_email}" ]]; then
      release_sender_input_default="${existing_name} <${existing_email}>"
    fi
  fi

  if [[ -f "${selected_dir}/publish.yaml" ]]; then
    existing_homebrew_tap_target_repo="$(extract_yaml_section_scalar "${selected_dir}/publish.yaml" "homebrew" "tap_deploy_target_repo")"
    existing_manual_pages_domain="$(extract_yaml_section_scalar "${selected_dir}/publish.yaml" "manual" "pages_domain")"
    existing_manual_site_url="$(extract_yaml_section_scalar "${selected_dir}/publish.yaml" "manual" "site_url")"
    existing_manual_target_repo="$(extract_yaml_section_scalar "${selected_dir}/publish.yaml" "manual" "target_repo")"
    existing_manual_target_dir="$(extract_yaml_section_scalar "${selected_dir}/publish.yaml" "manual" "target_dir")"
  fi
}

prompt_key_regeneration_choices() {
  local owner_key_changed="false"
  local recreate_age_release="false"
  local recreate_age_publish="false"

  age_pipeline_file="${dst_dir}/age-pipeline-keys.sops.yaml"
  git_secrets_file="${dst_dir}/git.sops.yaml"
  publish_secrets_file="${dst_dir}/publish.sops.yaml"
  renovate_secrets_file="${dst_dir}/renovate.sops.yaml"

  recreate_age_bundle="true"
  recreate_git_signing="true"
  recreate_publish_bundle="true"
  recreate_publish_cosign="true"
  recreate_publish_homebrew_tap="true"
  recreate_publish_manual_pages_deploy="true"
  manage_manual_pages_deploy_key="false"

  if [[ -f "${age_pipeline_file}" && -n "${existing_release_age_public}" && -n "${existing_publish_age_public}" ]]; then
    if prompt_yes_no "Release pipeline AGE key neu erstellen?" "no"; then
      recreate_age_release="true"
    fi
    if prompt_yes_no "Publish pipeline AGE key neu erstellen?" "no"; then
      recreate_age_publish="true"
    fi
    if [[ "${recreate_age_release}" == "true" || "${recreate_age_publish}" == "true" ]]; then
      recreate_age_bundle="true"
      echo "Pipeline AGE keys are stored together; release and publish pipeline AGE keys will both be regenerated." >&2
    else
      recreate_age_bundle="false"
    fi
  fi

  if [[ -f "${git_secrets_file}" && -n "${existing_git_signing_public_key}" ]]; then
    if prompt_yes_no "Git signing SSH key neu erstellen?" "no"; then
      recreate_git_signing="true"
    else
      recreate_git_signing="false"
    fi
  fi

  if [[ -f "${publish_secrets_file}" ]]; then
    if [[ -n "${existing_cosign_public_key_pem}" ]]; then
      if prompt_yes_no "Cosign key pair neu erstellen?" "no"; then
        recreate_publish_cosign="true"
      else
        recreate_publish_cosign="false"
      fi
    else
      recreate_publish_cosign="true"
      echo "Cosign public key is missing; selected key will be regenerated." >&2
    fi

    if [[ -n "${existing_homebrew_tap_public_key}" ]]; then
      if prompt_yes_no "Homebrew tap deploy SSH key neu erstellen?" "no"; then
        recreate_publish_homebrew_tap="true"
      else
        recreate_publish_homebrew_tap="false"
      fi
    else
      recreate_publish_homebrew_tap="true"
      echo "Homebrew tap deploy SSH public key is missing; selected key will be regenerated." >&2
    fi

    if [[ -n "${existing_manual_pages_deploy_public_key}" || -n "${existing_manual_pages_deploy_private_key}" ]]; then
      manage_manual_pages_deploy_key="true"
      if prompt_yes_no "Manual pages deploy SSH key neu erstellen?" "no"; then
        recreate_publish_manual_pages_deploy="true"
      else
        recreate_publish_manual_pages_deploy="false"
      fi
    else
      if prompt_yes_no "Manual pages deploy SSH key erzeugen?" "no"; then
        manage_manual_pages_deploy_key="true"
        recreate_publish_manual_pages_deploy="true"
      else
        manage_manual_pages_deploy_key="false"
        recreate_publish_manual_pages_deploy="false"
      fi
    fi

    if [[ "${recreate_publish_cosign}" == "true" || "${recreate_publish_homebrew_tap}" == "true" || ( "${manage_manual_pages_deploy_key}" == "true" && "${recreate_publish_manual_pages_deploy}" == "true" ) ]]; then
      recreate_publish_bundle="true"
    else
      recreate_publish_bundle="false"
    fi
  fi

  if [[ -n "${existing_owner_public_key}" && "${existing_owner_public_key}" != "${fork_public_key}" ]]; then
    owner_key_changed="true"
  fi

  # Existing encrypted files keep their original recipients. If owner key changes,
  # we must regenerate affected secrets so the new owner key can decrypt them.
  if [[ "${owner_key_changed}" == "true" ]]; then
    if [[ "${recreate_age_bundle}" == "false" || "${recreate_git_signing}" == "false" || "${recreate_publish_bundle}" == "false" ]]; then
      echo "Owner SSH public key changed. Encrypted secrets will be regenerated so recipients are updated." >&2
    fi
    recreate_age_bundle="true"
    recreate_git_signing="true"
    recreate_publish_bundle="true"
  fi

  # git.sops.yaml and publish.sops.yaml are encrypted to age recipients.
  # When AGE keys rotate, these files must be regenerated too.
  if [[ "${recreate_age_bundle}" == "true" ]]; then
    if [[ "${recreate_git_signing}" == "false" || "${recreate_publish_bundle}" == "false" ]]; then
      echo "AGE key rotation selected. Git signing and publish secret bundles will be regenerated for recipient consistency." >&2
    fi
    recreate_git_signing="true"
    recreate_publish_bundle="true"
  fi
}

load_existing_publish_secrets() {
  local decrypted_file
  local legacy_homebrew_tap_private_key

  if [[ ! -f "${publish_secrets_file}" ]]; then
    return
  fi

  decrypted_file="$(mktemp "${TMPDIR:-/tmp}/hydra-publish-secrets.XXXXXX")"

  if ! sops decrypt "${publish_secrets_file}" > "${decrypted_file}"; then
    rm -f "${decrypted_file}"
    echo "Could not decrypt existing ${publish_secrets_file#"${repo_root}/"}" >&2
    exit 1
  fi

  existing_cosign_private_key_pem="$(extract_yaml_section_block "${decrypted_file}" "cosign" "private_key")"
  existing_cosign_password="$(extract_yaml_section_scalar "${decrypted_file}" "cosign" "password")"
  existing_homebrew_tap_private_key="$(extract_yaml_section_block "${decrypted_file}" "homebrew" "tap_deploy_key")"
  if [[ -z "${existing_homebrew_tap_private_key}" ]]; then
    legacy_homebrew_tap_private_key="$(extract_yaml_section_block "${decrypted_file}" "homebrew" "tap_token")"
    if [[ -n "${legacy_homebrew_tap_private_key}" ]]; then
      existing_homebrew_tap_private_key="${legacy_homebrew_tap_private_key}"
    fi
  fi
  existing_manual_pages_deploy_private_key="$(extract_yaml_section_block "${decrypted_file}" "manual" "pages_deploy_key")"

  rm -f "${decrypted_file}"
}

validate_publish_key_material_availability() {
  local missing=0

  if [[ "${recreate_publish_cosign}" == "false" && ( -z "${existing_cosign_private_key_pem}" || -z "${existing_cosign_password}" ) ]]; then
    echo "Existing cosign key material is missing and cannot be kept while updating publish.sops.yaml." >&2
    echo "Re-run and answer 'yes' for 'Cosign key pair neu erstellen?' to continue." >&2
    missing=1
  fi

  if [[ "${recreate_publish_homebrew_tap}" == "false" && -z "${existing_homebrew_tap_private_key}" ]]; then
    echo "Existing Homebrew tap deploy private key is missing and cannot be kept while updating publish.sops.yaml." >&2
    echo "Re-run and answer 'yes' for 'Homebrew tap deploy SSH key neu erstellen?' to continue." >&2
    missing=1
  fi

  if [[ "${manage_manual_pages_deploy_key}" == "true" && "${recreate_publish_manual_pages_deploy}" == "false" && -z "${existing_manual_pages_deploy_private_key}" ]]; then
    echo "Existing manual pages deploy private key is missing and cannot be kept while updating publish.sops.yaml." >&2
    echo "Re-run and answer 'yes' for 'Manual pages deploy SSH key neu erstellen?' to continue." >&2
    missing=1
  fi

  if [[ "${missing}" -eq 1 ]]; then
    exit 1
  fi
}

prompt_inputs() {
  local fork_path_default=""

  fork_path_default="$(detect_default_fork_path || true)"
  fork_path="$(prompt_with_default "Fork path (owner/repo)" "${fork_path_default}")"

  while [[ ! "${fork_path}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; do
    echo "Invalid fork path '${fork_path}'. Expected format: owner/repo" >&2
    fork_path="$(prompt_with_default "Fork path (owner/repo)" "${fork_path_default}")"
  done

  load_existing_defaults_for_fork "${fork_path}"
  fork_public_key="$(prompt_with_default "Owner SSH public key" "${fork_public_key_default}")"
  release_sender_input="$(prompt_with_default "Release sender (user <email>)" "${release_sender_input_default}")"

  while [[ ! "${release_sender_input}" =~ ${release_sender_regex} ]]; do
    echo "Invalid sender '${release_sender_input}'. Expected format: user <email>" >&2
    release_sender_input="$(prompt_with_default "Release sender (user <email>)" "${release_sender_input_default}")"
  done

  release_sender_name="${BASH_REMATCH[1]}"
  release_sender_name="$(printf '%s' "${release_sender_name}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  release_sender_email="${BASH_REMATCH[2]}"

  if [[ -z "${fork_public_key}" ]]; then
    echo "Public key must not be empty" >&2
    exit 1
  fi

  if [[ -z "${release_sender_name}" || -z "${release_sender_email}" ]]; then
    echo "Sender user and email must not be empty" >&2
    exit 1
  fi
}

prompt_inputs

fork_owner="${fork_path%%/*}"
fork_repo="${fork_path##*/}"
if [[ -n "${existing_homebrew_tap_target_repo}" ]]; then
  homebrew_tap_target_repo="${existing_homebrew_tap_target_repo}"
else
  homebrew_tap_target_repo="${fork_owner}/homebrew-${fork_repo}"
fi

if [[ -n "${existing_manual_pages_domain}" ]]; then
  manual_pages_domain="${existing_manual_pages_domain}"
else
  manual_pages_domain="docs.hydra-gitops.org"
fi

if [[ -n "${existing_manual_site_url}" ]]; then
  manual_site_url="${existing_manual_site_url}"
else
  manual_site_url="https://docs.hydra-gitops.org/"
fi

if [[ -n "${existing_manual_target_repo}" ]]; then
  manual_target_repo="${existing_manual_target_repo}"
else
  manual_target_repo=""
fi

if [[ -n "${existing_manual_target_dir}" ]]; then
  manual_target_dir="${existing_manual_target_dir}"
else
  manual_target_dir=""
fi

dst_dir="${secrets_root}/repos/${fork_path}"

prompt_key_regeneration_choices

escape_regex() {
  printf '%s\n' "$1" | sed -e 's/[.[\*^$()+?{|}\\]/\\&/g'
}

fork_path_regex="$(escape_regex "${fork_path}")"

if [[ ! -f "${sops_config}" ]]; then
  echo "SOPS config not found: ${sops_config}" >&2
  exit 1
fi

if ! command -v age-keygen >/dev/null 2>&1; then
  echo "age-keygen is required" >&2
  exit 1
fi

if ! command -v ssh-keygen >/dev/null 2>&1; then
  echo "ssh-keygen is required" >&2
  exit 1
fi

if ! command -v sops >/dev/null 2>&1; then
  echo "sops is required" >&2
  exit 1
fi

if ! command -v yq >/dev/null 2>&1; then
  echo "yq is required" >&2
  exit 1
fi

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

  echo "hydra binary is required (used for 'hydra yq' and 'hydra cosign')" >&2
  exit 1
}

prepare_fork_state() {
  mkdir -p "${dst_dir}"
}

generate_age_keypair() {
  local label="$1"
  local output
  local private_key
  local public_key

  output="$(age-keygen)"
  private_key="$(printf '%s\n' "${output}" | awk '/^AGE-SECRET-KEY-/{print; exit}')"
  public_key="$(printf '%s\n' "${output}" | awk -F': ' '/^# public key: /{print $2; exit}')"

  if [[ -z "${private_key}" || -z "${public_key}" ]]; then
    echo "Could not generate ${label} age keypair" >&2
    exit 1
  fi

  printf '%s|%s\n' "${private_key}" "${public_key}"
}

rule_recipients_for_file() {
  local file_name="$1"
  local recipients=("${fork_public_key}")

  case "${file_name}" in
    git.sops.yaml)
      recipients+=("${release_age_public}")
      recipients+=("${publish_age_public}")
      ;;
    publish.sops.yaml)
      recipients+=("${publish_age_public}")
      ;;
  esac

  local idx
  local last_idx

  last_idx=$((${#recipients[@]} - 1))
  for idx in "${!recipients[@]}"; do
    if [[ "${idx}" -lt "${last_idx}" ]]; then
      printf '      %s,\n' "${recipients[$idx]}"
    else
      printf '      %s\n' "${recipients[$idx]}"
    fi
  done
}

create_plain_file() {
  local target_file="$1"
  local content="$2"

  printf '%s\n' "${content}" > "${target_file}"
  echo "Created ${target_file#"${repo_root}/"}"
}

indent_block() {
  local text="$1"
  local indent="$2"
  local pad=""

  printf -v pad '%*s' "${indent}" ''
  printf '%s\n' "${text}" | sed "s/^/${pad}/"
}

print_yaml_multiline_or_empty() {
  local key="$1"
  local text="$2"
  local indent="$3"

  if [[ -n "${text}" ]]; then
    printf '%s: |\n' "${key}"
    indent_block "${text}" "${indent}"
    return
  fi

  printf '%s: ""\n' "${key}"
}

build_publish_secrets_content() {
  local expr='
    .cosign.private_key = strenv(COSIGN_PRIVATE_KEY_PEM) |
    .cosign.password = strenv(COSIGN_PASSWORD) |
    .homebrew.tap_deploy_key = strenv(HOMEBREW_TAP_PRIVATE_KEY)
  '

  if [[ "${manage_manual_pages_deploy_key}" == "true" ]]; then
    expr+=' |
    .manual.pages_deploy_key = strenv(MANUAL_PAGES_DEPLOY_PRIVATE_KEY)
    '
  fi

  COSIGN_PRIVATE_KEY_PEM="${cosign_private_key_pem}" \
  COSIGN_PASSWORD="${cosign_password}" \
  HOMEBREW_TAP_PRIVATE_KEY="${homebrew_tap_private_key}" \
  MANUAL_PAGES_DEPLOY_PRIVATE_KEY="${manual_pages_deploy_private_key}" \
  yq eval -n "${expr}"
}

build_publish_secrets_content_from_existing() {
  local source_file="$1"
  local work_file

  work_file="$(mktemp "${TMPDIR:-/tmp}/hydra-publish-secrets-edit.XXXXXX")"
  cp "${source_file}" "${work_file}"

  if [[ "${recreate_publish_cosign}" == "true" ]]; then
    COSIGN_PRIVATE_KEY_PEM="${cosign_private_key_pem}" \
    COSIGN_PASSWORD="${cosign_password}" \
    yq eval -i '
      .cosign.private_key = strenv(COSIGN_PRIVATE_KEY_PEM) |
      .cosign.password = strenv(COSIGN_PASSWORD)
    ' "${work_file}"
  fi

  if [[ "${recreate_publish_homebrew_tap}" == "true" ]]; then
    HOMEBREW_TAP_PRIVATE_KEY="${homebrew_tap_private_key}" \
    yq eval -i '.homebrew.tap_deploy_key = strenv(HOMEBREW_TAP_PRIVATE_KEY)' "${work_file}"
  fi

  if [[ "${manage_manual_pages_deploy_key}" == "true" && "${recreate_publish_manual_pages_deploy}" == "true" ]]; then
    MANUAL_PAGES_DEPLOY_PRIVATE_KEY="${manual_pages_deploy_private_key}" \
    yq eval -i '.manual.pages_deploy_key = strenv(MANUAL_PAGES_DEPLOY_PRIVATE_KEY)' "${work_file}"
  fi

  if [[ "${manage_manual_pages_deploy_key}" != "true" ]]; then
    yq eval -i 'del(.manual.pages_deploy_key)' "${work_file}"
  fi

  cat "${work_file}"
  rm -f "${work_file}"
}

generate_random_password() {
  local length="$1"
  local value

  value="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${length}")"
  if [[ -z "${value}" ]]; then
    echo "Could not generate random password" >&2
    exit 1
  fi

  printf '%s\n' "${value}"
}

generate_cosign_keypair() {
  local work_dir
  local prefix

  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/hydra-cosign.XXXXXX")"
  prefix="${work_dir}/cosign"
  cosign_password="$(generate_random_password 32)"

  if ! COSIGN_PASSWORD="${cosign_password}" "${hydra_bin}" cosign generate-key-pair --output-key-prefix "${prefix}" >/dev/null 2>&1; then
    rm -rf "${work_dir}"
    echo "Could not generate cosign key pair" >&2
    exit 1
  fi

  if [[ ! -f "${prefix}.key" || ! -f "${prefix}.pub" ]]; then
    rm -rf "${work_dir}"
    echo "Cosign key pair files are missing" >&2
    exit 1
  fi

  cosign_private_key_pem="$(cat "${prefix}.key")"
  cosign_public_key_pem="$(cat "${prefix}.pub")"
  rm -rf "${work_dir}"

  if [[ -z "${cosign_private_key_pem}" || -z "${cosign_public_key_pem}" ]]; then
    echo "Generated cosign keys are empty" >&2
    exit 1
  fi
}

generate_git_signing_keypair() {
  local work_dir
  local key_file

  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/hydra-git-signing.XXXXXX")"
  key_file="${work_dir}/id_ed25519"

  if ! ssh-keygen -q -t ed25519 -N "" -f "${key_file}" -C "${release_sender_email}" >/dev/null 2>&1; then
    rm -rf "${work_dir}"
    echo "Could not generate git signing SSH key pair" >&2
    exit 1
  fi

  if [[ ! -f "${key_file}" || ! -f "${key_file}.pub" ]]; then
    rm -rf "${work_dir}"
    echo "Git signing SSH key pair files are missing" >&2
    exit 1
  fi

  git_signing_private_key="$(cat "${key_file}")"
  git_signing_public_key="$(cat "${key_file}.pub")"
  rm -rf "${work_dir}"

  if [[ -z "${git_signing_private_key}" || -z "${git_signing_public_key}" ]]; then
    echo "Generated git signing SSH keys are empty" >&2
    exit 1
  fi
}

generate_homebrew_tap_keypair() {
  local work_dir
  local key_file

  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/hydra-homebrew-tap.XXXXXX")"
  key_file="${work_dir}/id_ed25519"

  if ! ssh-keygen -q -t ed25519 -N "" -f "${key_file}" -C "hydra-homebrew-tap" >/dev/null 2>&1; then
    rm -rf "${work_dir}"
    echo "Could not generate Homebrew tap deploy SSH key pair" >&2
    exit 1
  fi

  if [[ ! -f "${key_file}" || ! -f "${key_file}.pub" ]]; then
    rm -rf "${work_dir}"
    echo "Homebrew tap deploy SSH key pair files are missing" >&2
    exit 1
  fi

  homebrew_tap_private_key="$(cat "${key_file}")"
  homebrew_tap_public_key="$(cat "${key_file}.pub")"
  rm -rf "${work_dir}"

  if [[ -z "${homebrew_tap_private_key}" || -z "${homebrew_tap_public_key}" ]]; then
    echo "Generated Homebrew tap deploy SSH keys are empty" >&2
    exit 1
  fi
}

generate_manual_pages_deploy_keypair() {
  local work_dir
  local key_file

  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/hydra-manual-pages.XXXXXX")"
  key_file="${work_dir}/id_ed25519"

  if ! ssh-keygen -q -t ed25519 -N "" -f "${key_file}" -C "hydra-manual-pages-deploy" >/dev/null 2>&1; then
    rm -rf "${work_dir}"
    echo "Could not generate manual pages deploy SSH key pair" >&2
    exit 1
  fi

  if [[ ! -f "${key_file}" || ! -f "${key_file}.pub" ]]; then
    rm -rf "${work_dir}"
    echo "Manual pages deploy SSH key pair files are missing" >&2
    exit 1
  fi

  manual_pages_deploy_private_key="$(cat "${key_file}")"
  manual_pages_deploy_public_key="$(cat "${key_file}.pub")"
  rm -rf "${work_dir}"

  if [[ -z "${manual_pages_deploy_private_key}" || -z "${manual_pages_deploy_public_key}" ]]; then
    echo "Generated manual pages deploy SSH keys are empty" >&2
    exit 1
  fi
}

write_encrypted_file() {
  local file_name="$1"
  local content="$2"
  local target_file="${dst_dir}/${file_name}"
  local relative_file=".github/secrets/repos/${fork_path}/${file_name}"
  local target_state="Created"
  local plain_file
  local encrypted_file
  local editor_script

  plain_file="$(mktemp "${TMPDIR:-/tmp}/hydra-sops-plain.XXXXXX")"
  printf '%s\n' "${content}" > "${plain_file}"

  if [[ -f "${target_file}" ]]; then
    target_state="Updated"
    editor_script="$(mktemp "${TMPDIR:-/tmp}/hydra-sops-editor.XXXXXX")"

    cat > "${editor_script}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

source_file="${SOPS_EDITOR_SOURCE_FILE:?missing source file}"
target_file="${1:?missing target file}"

yq eval '.' "${source_file}" > "${target_file}"
EOF
    chmod +x "${editor_script}"

    if ! SOPS_EDITOR_SOURCE_FILE="${plain_file}" SOPS_EDITOR="${editor_script}" sops edit "${target_file}" >/dev/null; then
      rm -f "${plain_file}" "${editor_script}"
      echo "Failed to write encrypted file: ${target_file}" >&2
      exit 1
    fi

    rm -f "${plain_file}" "${editor_script}"
    echo "${target_state} ${target_file#"${repo_root}/"}"
    return
  fi

  encrypted_file="$(mktemp "${TMPDIR:-/tmp}/hydra-sops-encrypted.XXXXXX")"

  if ! sops encrypt --filename-override "${relative_file}" "${plain_file}" > "${encrypted_file}"; then
    rm -f "${plain_file}" "${encrypted_file}"
    echo "Failed to write encrypted file: ${target_file}" >&2
    exit 1
  fi

  mv "${encrypted_file}" "${target_file}"
  rm -f "${plain_file}"
  echo "${target_state} ${target_file#"${repo_root}/"}"
}

upsert_rule() {
  local file_name="$1"
  local recipients_block="$2"
  local escaped_file_name
  local path_regex_value
  local tmp_file

  escaped_file_name="$(escape_regex "${file_name}")"
  path_regex_value="^\\.github/secrets/repos/${fork_path_regex}/${escaped_file_name}$"

  tmp_file="$(mktemp "${TMPDIR:-/tmp}/hydra-sops-rules.XXXXXX")"

  RULE_PATH_REGEX_VALUE="${path_regex_value}" \
  RULE_RECIPIENTS="${recipients_block}" \
  awk '
    function flush_rule() {
      if (!in_rule) {
        return
      }

      if (drop_rule) {
        printf "  - path_regex: %s\n", ENVIRON["RULE_PATH_REGEX_VALUE"]
        printf "    age: >-\n"
        recipients = ENVIRON["RULE_RECIPIENTS"]
        printf "%s", recipients
        if (recipients !~ /\n$/) {
          printf "\n"
        }
        replaced = 1
      } else {
        printf "%s", rule_buf
      }

      rule_buf = ""
      in_rule = 0
      drop_rule = 0
    }

    {
      if ($0 ~ /^  - path_regex: /) {
        current_path_regex = $0
        sub(/^  - path_regex:[[:space:]]*/, "", current_path_regex)

        flush_rule()
        in_rule = 1
        rule_buf = $0 ORS

        if (current_path_regex == ENVIRON["RULE_PATH_REGEX_VALUE"]) {
          drop_rule = 1
        }
        next
      }

      if (in_rule) {
        rule_buf = rule_buf $0 ORS
        next
      }

      print
    }

    END {
      flush_rule()

      if (!replaced) {
        printf "  - path_regex: %s\n", ENVIRON["RULE_PATH_REGEX_VALUE"]
        printf "    age: >-\n"
        recipients = ENVIRON["RULE_RECIPIENTS"]
        printf "%s", recipients
        if (recipients !~ /\n$/) {
          printf "\n"
        }
      }
    }
  ' "${sops_config}" > "${tmp_file}"

  mv "${tmp_file}" "${sops_config}"
  echo "Updated .sops.yaml rule for ${fork_path}/${file_name}"
}

resolve_hydra_bin
prepare_fork_state

release_age_private=""
release_age_public=""
publish_age_private=""
publish_age_public=""

if [[ "${recreate_age_bundle}" == "true" ]]; then
  release_age_pair="$(generate_age_keypair "release")"
  release_age_private="${release_age_pair%%|*}"
  release_age_public="${release_age_pair##*|}"

  publish_age_pair="$(generate_age_keypair "publish")"
  publish_age_private="${publish_age_pair%%|*}"
  publish_age_public="${publish_age_pair##*|}"
else
  release_age_public="${existing_release_age_public}"
  publish_age_public="${existing_publish_age_public}"
fi

if [[ "${recreate_publish_bundle}" == "true" && -f "${publish_secrets_file}" ]]; then
  load_existing_publish_secrets
  validate_publish_key_material_availability
fi

cosign_private_key_pem=""
cosign_public_key_pem=""
cosign_password=""
if [[ "${recreate_publish_cosign}" == "true" ]]; then
  generate_cosign_keypair
else
  cosign_private_key_pem="${existing_cosign_private_key_pem}"
  cosign_public_key_pem="${existing_cosign_public_key_pem}"
  cosign_password="${existing_cosign_password}"
fi

git_signing_private_key=""
git_signing_public_key=""
if [[ "${recreate_git_signing}" == "true" ]]; then
  generate_git_signing_keypair
else
  git_signing_public_key="${existing_git_signing_public_key}"
fi

homebrew_tap_private_key=""
homebrew_tap_public_key=""
if [[ "${recreate_publish_homebrew_tap}" == "true" ]]; then
  generate_homebrew_tap_keypair
else
  homebrew_tap_private_key="${existing_homebrew_tap_private_key}"
  homebrew_tap_public_key="${existing_homebrew_tap_public_key}"
fi

manual_pages_deploy_private_key=""
manual_pages_deploy_public_key=""
if [[ "${manage_manual_pages_deploy_key}" == "true" && "${recreate_publish_manual_pages_deploy}" == "true" ]]; then
  generate_manual_pages_deploy_keypair
elif [[ "${manage_manual_pages_deploy_key}" == "true" ]]; then
  manual_pages_deploy_private_key="${existing_manual_pages_deploy_private_key}"
  manual_pages_deploy_public_key="${existing_manual_pages_deploy_public_key}"
fi

if [[ -z "${release_age_public}" || -z "${publish_age_public}" ]]; then
  echo "AGE public keys are required. Please regenerate AGE pipeline keys." >&2
  exit 1
fi

if [[ -z "${git_signing_public_key}" ]]; then
  echo "Git signing SSH public key is required. Please regenerate git signing key." >&2
  exit 1
fi

if [[ -z "${cosign_public_key_pem}" || -z "${homebrew_tap_public_key}" ]]; then
  echo "Cosign and Homebrew tap deploy public keys are required. Please regenerate publish keys." >&2
  exit 1
fi
git_recipients="$(rule_recipients_for_file "git.sops.yaml")"
publish_recipients="$(rule_recipients_for_file "publish.sops.yaml")"
renovate_recipients="$(rule_recipients_for_file "renovate.sops.yaml")"
pipeline_recipients="$(rule_recipients_for_file "age-pipeline-keys.sops.yaml")"

github_actions_secrets_url="https://github.com/${fork_path}/settings/secrets/actions"
github_actions_secret_release_url="${github_actions_secrets_url}/new?name=SOPS_AGE_KEY_RELEASE"
github_actions_secret_publish_url="${github_actions_secrets_url}/new?name=SOPS_AGE_KEY_PUBLISH"
github_actions_secret_git_signing_url="${github_actions_secrets_url}/new?name=SEMANTIC_RELEASE_GIT_SIGNING_KEY"
github_actions_secret_homebrew_url="${github_actions_secrets_url}/new?name=HOMEBREW_TAP_DEPLOY_KEY"
github_environments_url="https://github.com/${fork_path}/settings/environments"
github_environment_new_url="${github_environments_url}/new"
github_environment_publish_url="${github_environments_url}/publish"
github_environment_release_url="${github_environments_url}/release"

manual_pages_public_keys_block=""
if [[ -n "${manual_pages_deploy_public_key}" ]]; then
  manual_pages_public_keys_block="

manual_pages:
  public_key_openssh: \"${manual_pages_deploy_public_key}\""
fi

publish_manual_target_repo_block=""
if [[ -n "${manual_target_repo}" ]]; then
  publish_manual_target_repo_block="
  target_repo: \"${manual_target_repo}\""
fi

publish_manual_target_dir_block=""
if [[ -n "${manual_target_dir}" ]]; then
  publish_manual_target_dir_block="
  target_dir: \"${manual_target_dir}\""
fi

create_plain_file "${dst_dir}/public-keys.yaml" "owner_ssh_public_key: \"${fork_public_key}\"

semantic_release_age:
  public_key: ${release_age_public}
  github_secret_name: SOPS_AGE_KEY_RELEASE

publish_age:
  public_key: ${publish_age_public}
  github_secret_name: SOPS_AGE_KEY_PUBLISH

git_signing:
  public_key_openssh: \"${git_signing_public_key}\"
  github_secret_name: SEMANTIC_RELEASE_GIT_SIGNING_KEY

cosign:
  public_key_pem: |
$(indent_block "${cosign_public_key_pem}" 4)

homebrew_tap:
  public_key_openssh: \"${homebrew_tap_public_key}\"
  github_secret_name: HOMEBREW_TAP_DEPLOY_KEY${manual_pages_public_keys_block}"

create_plain_file "${dst_dir}/git.yaml" "user:
  name: \"${release_sender_name}\"
  email: \"${release_sender_email}\""

create_plain_file "${dst_dir}/publish.yaml" "homebrew:
  tap_deploy_target_repo: \"${homebrew_tap_target_repo}\"
manual:
  pages_domain: \"${manual_pages_domain}\"
  site_url: \"${manual_site_url}\"${publish_manual_target_repo_block}${publish_manual_target_dir_block}"

create_plain_file "${dst_dir}/README.md" "# Secrets setup for ${fork_path}

This directory contains repository scoped secret material for \`${fork_path}\`.

## Files

- \`public-keys.yaml\`: non-sensitive public keys and target GitHub secret names.
- \`git.yaml\`: non-sensitive git identity settings (user/author/committer).
- \`publish.yaml\`: non-sensitive publish settings (Homebrew tap deploy target repository plus manual GitHub Pages domain/site URL and optional target repo/target dir).
- \`age-pipeline-keys.sops.yaml\`: encrypted age private keys for CI decryption.
- \`git.sops.yaml\`: encrypted semantic-release signing private key.
- \`publish.sops.yaml\`: encrypted cosign private key, Homebrew tap deploy key, and optional manual pages deploy SSH key.
- \`renovate.sops.yaml\`: encrypted Renovate token source value.

\`git.sops.yaml\` is encrypted for both pipeline keys (\`age_keys.release.private_key\` and \`age_keys.publish.private_key\`) so either pipeline context can decrypt it.

## GitHub repository secrets to configure

- \`SEMANTIC_RELEASE_GIT_SIGNING_KEY\`: public key in \`public-keys.yaml\` at \`git_signing.public_key_openssh\`.
- \`HOMEBREW_TAP_DEPLOY_KEY\`: public key in \`public-keys.yaml\` at \`homebrew_tap.public_key_openssh\`.

### GitHub links to create or update repository secrets

- Actions secrets overview: ${github_actions_secrets_url}
- Create/update \`SEMANTIC_RELEASE_GIT_SIGNING_KEY\`: ${github_actions_secret_git_signing_url}
- Create/update \`HOMEBREW_TAP_DEPLOY_KEY\`: ${github_actions_secret_homebrew_url}

## GitHub environments configuration

CI uses two GitHub Actions environments: \`publish\` and \`release\`.

### Required setup

1. Create environments \`publish\` and \`release\`.
2. Configure deployment branch policies:
  - \`publish\` must allow only tag pattern \`v*\`.
  - \`release\` must allow only branch \`main\`.
3. Configure environment secrets:
  - \`publish\`: only \`SOPS_AGE_KEY_PUBLISH\` (from \`age-pipeline-keys.sops.yaml\` -> \`age_keys.publish.private_key\`).
  - \`release\`: only \`SOPS_AGE_KEY_RELEASE\` (from \`age-pipeline-keys.sops.yaml\` -> \`age_keys.release.private_key\`).

### GitHub links for environment setup

- Environments overview: ${github_environments_url}
- Create new environment: ${github_environment_new_url}
- Configure \`publish\` environment: ${github_environment_publish_url}
- Configure \`release\` environment: ${github_environment_release_url}

## Configure with gh CLI

- Ensure prerequisites are available: \`gh\`, \`sops\`, and \`hydra\`.
- Run \`gh auth login\` if needed.
- Run \`scripts/configure-github.sh ${fork_path}\`.
- Optional custom secrets path: \`scripts/configure-github.sh ${fork_path} .github/secrets/repos/${fork_path}\`.

The script configures:

- environments \`publish\` and \`release\`
- deployment branch policies (\`publish\` -> tag \`v*\`, \`release\` -> branch \`main\`)
- environment secrets \`SOPS_AGE_KEY_PUBLISH\` and \`SOPS_AGE_KEY_RELEASE\`
- repository secrets \`SEMANTIC_RELEASE_GIT_SIGNING_KEY\` and \`HOMEBREW_TAP_DEPLOY_KEY\`
- active ruleset requiring signed commits on the default branch
- validation via \`scripts/check-github-repository-settings.sh\`

## Values that must be filled manually

- \`renovate.sops.yaml\` -> \`renovate.token\`.

## Already generated automatically

- Age key pairs for \`SOPS_AGE_KEY_RELEASE\` and \`SOPS_AGE_KEY_PUBLISH\`.
- SSH git signing key pair:
  - public key in \`public-keys.yaml\` at \`git_signing.public_key_openssh\`
  - private key in \`git.sops.yaml\` at \`git_signing.private_key\`
- Cosign key pair:
  - public key in \`public-keys.yaml\` at \`cosign.public_key_pem\`
  - private key and password in \`publish.sops.yaml\` at \`cosign.private_key\` and \`cosign.password\`
- Homebrew tap deploy SSH key pair:
  - public key in \`public-keys.yaml\` at \`homebrew_tap.public_key_openssh\`
  - private key in \`publish.sops.yaml\` at \`homebrew.tap_deploy_key\`
$(if [[ -n "${manual_pages_deploy_public_key}" ]]; then cat <<EOF
- Manual pages deploy SSH key pair:
  - public key in \`public-keys.yaml\` at \`manual_pages.public_key_openssh\`
  - private key in \`publish.sops.yaml\` at \`manual.pages_deploy_key\`
EOF
fi)
- Homebrew tap deploy target repository in \`publish.yaml\` at \`homebrew.tap_deploy_target_repo\`.
- Manual pages domain in \`publish.yaml\` at \`manual.pages_domain\`.
- Manual pages site URL in \`publish.yaml\` at \`manual.site_url\`.
$(if [[ -n "${manual_target_repo}" ]]; then printf '%s\n' "- Manual pages target repository in \`publish.yaml\` at \`manual.target_repo\`."; fi)
$(if [[ -n "${manual_target_dir}" ]]; then printf '%s\n' "- Manual pages target directory in \`publish.yaml\` at \`manual.target_dir\`."; fi)"

upsert_rule "git.sops.yaml" "${git_recipients}"
upsert_rule "publish.sops.yaml" "${publish_recipients}"
upsert_rule "renovate.sops.yaml" "${renovate_recipients}"
upsert_rule "age-pipeline-keys.sops.yaml" "${pipeline_recipients}"

if [[ "${recreate_age_bundle}" == "true" ]]; then
  write_encrypted_file "age-pipeline-keys.sops.yaml" "age_keys:
  release:
    github_secret_name: SOPS_AGE_KEY_RELEASE
    private_key: \"${release_age_private}\"
  publish:
    github_secret_name: SOPS_AGE_KEY_PUBLISH
    private_key: \"${publish_age_private}\""
else
  echo "Kept existing ${age_pipeline_file#"${repo_root}/"}"
fi

if [[ "${recreate_publish_bundle}" == "true" ]]; then
  if [[ -f "${publish_secrets_file}" ]]; then
    publish_plain_file="$(mktemp "${TMPDIR:-/tmp}/hydra-publish-secrets-plain.XXXXXX")"
    if ! sops decrypt "${publish_secrets_file}" > "${publish_plain_file}"; then
      rm -f "${publish_plain_file}"
      echo "Could not decrypt existing ${publish_secrets_file#"${repo_root}/"}" >&2
      exit 1
    fi
    publish_secrets_content="$(build_publish_secrets_content_from_existing "${publish_plain_file}")"
    rm -f "${publish_plain_file}"
  else
    publish_secrets_content="$(build_publish_secrets_content)"
  fi
  write_encrypted_file "publish.sops.yaml" "${publish_secrets_content}"
else
  echo "Kept existing ${publish_secrets_file#"${repo_root}/"}"
fi

if [[ "${recreate_git_signing}" == "true" ]]; then
  write_encrypted_file "git.sops.yaml" "git_signing:
  private_key: |
$(indent_block "${git_signing_private_key}" 4)"
else
  echo "Kept existing ${git_secrets_file#"${repo_root}/"}"
fi

if [[ -f "${renovate_secrets_file}" ]]; then
  echo "Kept existing ${renovate_secrets_file#"${repo_root}/"}"
else
  write_encrypted_file "renovate.sops.yaml" "renovate:
  token: \"<place renovate token here>\""
fi

echo "Updated .sops.yaml for ${fork_path}"
echo "Fork secret templates created for ${fork_path}"
echo
echo "Additional GitHub key assignment steps:"
echo "1) Assign the semantic-release signing SSH key to a GitHub user account"
echo "   - User (git.yaml -> user.name): ${release_sender_name}"
echo "   - Email (git.yaml -> user.email): ${release_sender_email}"
echo "   - Signing SSH public key (public-keys.yaml -> git_signing.public_key_openssh): ${git_signing_public_key}"
echo "2) Add the Homebrew tap deploy SSH key as deploy key to the tap repository"
echo "   - Target repository (publish.yaml -> homebrew.tap_deploy_target_repo): ${homebrew_tap_target_repo}"
echo "   - Deploy SSH public key (public-keys.yaml -> homebrew_tap.public_key_openssh): ${homebrew_tap_public_key}"
if [[ -n "${manual_pages_deploy_public_key}" ]]; then
  echo "3) Add the manual pages deploy SSH key as deploy key to the manual target repository"
  echo "   - Target repository (publish.yaml -> manual.target_repo): ${manual_target_repo:-${fork_path}}"
  echo "   - Deploy SSH public key (public-keys.yaml -> manual_pages.public_key_openssh): ${manual_pages_deploy_public_key}"
fi
