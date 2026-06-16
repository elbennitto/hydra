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

recreate_age_bundle="true"
recreate_git_signing="true"
recreate_publish_bundle="true"

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

    in_section && $0 ~ "^[[:space:]]*" key ":[[:space:]]*\\|[[:space:]]*$" {
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

  if [[ -f "${selected_dir}/public-keys.yaml" ]]; then
    fork_public_key_default="$(extract_yaml_quoted_value "${selected_dir}/public-keys.yaml" "owner_ssh_public_key")"
    existing_owner_public_key="${fork_public_key_default}"
    existing_release_age_public="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "semantic_release_age" "public_key")"
    existing_publish_age_public="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "publish_age" "public_key")"
    existing_git_signing_public_key="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "git_signing" "public_key_openssh")"
    existing_cosign_public_key_pem="$(extract_yaml_section_block "${selected_dir}/public-keys.yaml" "cosign" "public_key_pem")"
    existing_homebrew_tap_public_key="$(extract_yaml_section_scalar "${selected_dir}/public-keys.yaml" "homebrew_tap" "public_key_openssh")"
  fi

  if [[ -f "${selected_dir}/git.yaml" ]]; then
    existing_name="$(extract_yaml_quoted_value "${selected_dir}/git.yaml" "  name")"
    existing_email="$(extract_yaml_quoted_value "${selected_dir}/git.yaml" "  email")"
    if [[ -n "${existing_name}" && -n "${existing_email}" ]]; then
      release_sender_input_default="${existing_name} <${existing_email}>"
    fi
  fi
}

prompt_key_regeneration_choices() {
  local owner_key_changed="false"
  local recreate_age_release="false"
  local recreate_age_publish="false"
  local recreate_cosign="false"
  local recreate_homebrew_tap="false"

  age_pipeline_file="${dst_dir}/age-pipeline-keys.sops.yaml"
  git_secrets_file="${dst_dir}/git.sops.yaml"
  publish_secrets_file="${dst_dir}/publish.sops.yaml"
  renovate_secrets_file="${dst_dir}/renovate.sops.yaml"

  recreate_age_bundle="true"
  recreate_git_signing="true"
  recreate_publish_bundle="true"

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

  if [[ -f "${publish_secrets_file}" && -n "${existing_cosign_public_key_pem}" && -n "${existing_homebrew_tap_public_key}" ]]; then
    if prompt_yes_no "Cosign key pair neu erstellen?" "no"; then
      recreate_cosign="true"
    fi
    if prompt_yes_no "Homebrew tap deploy SSH key neu erstellen?" "no"; then
      recreate_homebrew_tap="true"
    fi
    if [[ "${recreate_cosign}" == "true" || "${recreate_homebrew_tap}" == "true" ]]; then
      recreate_publish_bundle="true"
      echo "Cosign and Homebrew deploy keys are stored together; both will be regenerated." >&2
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
homebrew_tap_target_repo="${fork_owner}/homebrew-${fork_repo}"
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

reset_existing_fork_state() {
  local tmp_file
  tmp_file="$(mktemp "${TMPDIR:-/tmp}/hydra-sops-rules.XXXXXX")"

  FORK_PATH="${fork_path}" awk '
    function flush_rule() {
      if (!in_rule) {
        return
      }
      if (!drop_rule) {
        printf "%s", rule_buf
      }
      rule_buf = ""
      in_rule = 0
      drop_rule = 0
    }

    {
      if ($0 ~ /^  - path_regex: /) {
        flush_rule()
        in_rule = 1
        rule_buf = $0 ORS
        if (index($0, "/repos/" ENVIRON["FORK_PATH"] "/") > 0) {
          drop_rule = 1
        }
        next
      }

      if (in_rule) {
        rule_buf = rule_buf $0 ORS
        if (index($0, "/repos/" ENVIRON["FORK_PATH"] "/") > 0) {
          drop_rule = 1
        }
        next
      }

      print
    }

    END {
      flush_rule()
    }
  ' "${sops_config}" > "${tmp_file}"

  mv "${tmp_file}" "${sops_config}"
  echo "Removed existing .sops.yaml rules for ${fork_path}"

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

create_encrypted_file() {
  local file_name="$1"
  local content="$2"
  local target_file="${dst_dir}/${file_name}"
  local relative_file=".github/secrets/repos/${fork_path}/${file_name}"

  if ! printf '%s\n' "${content}" | sops encrypt --filename-override "${relative_file}" /dev/stdin > "${target_file}"; then
    rm -f "${target_file}"
    echo "Failed to create encrypted file: ${target_file}" >&2
    exit 1
  fi

  echo "Created ${target_file#"${repo_root}/"}"
}

append_rule() {
  local file_name="$1"
  local recipients_block="$2"
  local escaped_file_name

  escaped_file_name="$(escape_regex "${file_name}")"

  cat >> "${sops_config}" <<EOF
  - path_regex: ^\\.github/secrets/repos/${fork_path_regex}/${escaped_file_name}$
    age: >-
${recipients_block}
EOF
}

resolve_hydra_bin
reset_existing_fork_state

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

cosign_private_key_pem=""
cosign_public_key_pem=""
cosign_password=""
if [[ "${recreate_publish_bundle}" == "true" ]]; then
  generate_cosign_keypair
else
  cosign_public_key_pem="${existing_cosign_public_key_pem}"
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
if [[ "${recreate_publish_bundle}" == "true" ]]; then
  generate_homebrew_tap_keypair
else
  homebrew_tap_public_key="${existing_homebrew_tap_public_key}"
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
  github_secret_name: HOMEBREW_TAP_DEPLOY_KEY"

create_plain_file "${dst_dir}/git.yaml" "user:
  name: \"${release_sender_name}\"
  email: \"${release_sender_email}\""

create_plain_file "${dst_dir}/publish.yaml" "homebrew:
  tap_deploy_target_repo: \"${homebrew_tap_target_repo}\""

create_plain_file "${dst_dir}/README.md" "# Secrets setup for ${fork_path}

This directory contains repository scoped secret material for \`${fork_path}\`.

## Files

- \`public-keys.yaml\`: non-sensitive public keys and target GitHub secret names.
- \`git.yaml\`: non-sensitive git identity settings (user/author/committer).
- \`publish.yaml\`: non-sensitive publish settings (Homebrew tap deploy target repository).
- \`age-pipeline-keys.sops.yaml\`: encrypted age private keys for CI decryption.
- \`git.sops.yaml\`: encrypted semantic-release signing private key.
- \`publish.sops.yaml\`: encrypted cosign private key and Homebrew tap deploy key.
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
- Homebrew tap deploy target repository in \`publish.yaml\` at \`homebrew.tap_deploy_target_repo\`."

append_rule "git.sops.yaml" "${git_recipients}"
append_rule "publish.sops.yaml" "${publish_recipients}"
append_rule "renovate.sops.yaml" "${renovate_recipients}"
append_rule "age-pipeline-keys.sops.yaml" "${pipeline_recipients}"

if [[ "${recreate_age_bundle}" == "true" ]]; then
  create_encrypted_file "age-pipeline-keys.sops.yaml" "age_keys:
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
  create_encrypted_file "publish.sops.yaml" "cosign:
  private_key: |
$(indent_block "${cosign_private_key_pem}" 4)
  password: \"${cosign_password}\"
homebrew:
  tap_deploy_key: |
$(indent_block "${homebrew_tap_private_key}" 4)"
else
  echo "Kept existing ${publish_secrets_file#"${repo_root}/"}"
fi

if [[ "${recreate_git_signing}" == "true" ]]; then
  create_encrypted_file "git.sops.yaml" "git_signing:
  private_key: |
$(indent_block "${git_signing_private_key}" 4)"
else
  echo "Kept existing ${git_secrets_file#"${repo_root}/"}"
fi

if [[ -f "${renovate_secrets_file}" ]]; then
  echo "Kept existing ${renovate_secrets_file#"${repo_root}/"}"
else
  create_encrypted_file "renovate.sops.yaml" "renovate:
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
