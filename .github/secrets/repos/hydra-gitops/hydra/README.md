# Secrets setup for hydra-gitops/hydra

This directory contains repository scoped secret material for `hydra-gitops/hydra`.

## Files

- `public-keys.yaml`: non-sensitive public keys and target GitHub secret names.
- `git.yaml`: non-sensitive git identity settings (user/author/committer).
- `publish.yaml`: non-sensitive publish settings (Homebrew tap deploy target repository).
- `age-pipeline-keys.sops.yaml`: encrypted age private keys for CI decryption.
- `git.sops.yaml`: encrypted semantic-release signing private key.
- `publish.sops.yaml`: encrypted cosign private key and Homebrew tap deploy key.
- `renovate.sops.yaml`: encrypted Renovate token source value.

`git.sops.yaml` is encrypted for both pipeline keys (`age_keys.release.private_key` and `age_keys.publish.private_key`) so either pipeline context can decrypt it.

## GitHub repository secrets to configure

- `SEMANTIC_RELEASE_GIT_SIGNING_KEY`: public key in `public-keys.yaml` at `git_signing.public_key_openssh`.
- `HOMEBREW_TAP_DEPLOY_KEY`: public key in `public-keys.yaml` at `homebrew_tap.public_key_openssh`.

### GitHub links to create or update repository secrets

- Actions secrets overview: https://github.com/hydra-gitops/hydra/settings/secrets/actions
- Create/update `SEMANTIC_RELEASE_GIT_SIGNING_KEY`: https://github.com/hydra-gitops/hydra/settings/secrets/actions/new?name=SEMANTIC_RELEASE_GIT_SIGNING_KEY
- Create/update `HOMEBREW_TAP_DEPLOY_KEY`: https://github.com/hydra-gitops/hydra/settings/secrets/actions/new?name=HOMEBREW_TAP_DEPLOY_KEY

## GitHub environments configuration

CI uses two GitHub Actions environments: `publish` and `release`.

### Required setup

1. Create environments `publish` and `release`.
2. Configure deployment branch policies:
  - `publish` must allow only tag pattern `v*`.
  - `release` must allow only branch `main`.
3. Configure environment secrets:
  - `publish`: only `SOPS_AGE_KEY_PUBLISH` (from `age-pipeline-keys.sops.yaml` -> `age_keys.publish.private_key`).
  - `release`: only `SOPS_AGE_KEY_RELEASE` (from `age-pipeline-keys.sops.yaml` -> `age_keys.release.private_key`).

### GitHub links for environment setup

- Environments overview: https://github.com/hydra-gitops/hydra/settings/environments
- Create new environment: https://github.com/hydra-gitops/hydra/settings/environments/new
- Configure `publish` environment: https://github.com/hydra-gitops/hydra/settings/environments/publish
- Configure `release` environment: https://github.com/hydra-gitops/hydra/settings/environments/release

## Configure with gh CLI

- Ensure prerequisites are available: `gh`, `sops`, and `hydra`.
- Run `gh auth login` if needed.
- Run `scripts/configure-github.sh hydra-gitops/hydra`.
- Optional custom secrets path: `scripts/configure-github.sh hydra-gitops/hydra .github/secrets/repos/hydra-gitops/hydra`.

The script configures:

- environments `publish` and `release`
- deployment branch policies (`publish` -> tag `v*`, `release` -> branch `main`)
- environment secrets `SOPS_AGE_KEY_PUBLISH` and `SOPS_AGE_KEY_RELEASE`
- repository secrets `SEMANTIC_RELEASE_GIT_SIGNING_KEY` and `HOMEBREW_TAP_DEPLOY_KEY`
- active ruleset requiring signed commits on the default branch
- validation via `scripts/check-github-repository-settings.sh`

## Values that must be filled manually

- `git.yaml` -> `user.name`, `user.email`, optional `author.*`, and optional `committer.*`.
- `renovate.sops.yaml` -> `renovate.token`.

## Already generated automatically

- Age key pairs for `SOPS_AGE_KEY_RELEASE` and `SOPS_AGE_KEY_PUBLISH`.
- SSH git signing key pair:
  - public key in `public-keys.yaml` at `git_signing.public_key_openssh`
  - private key in `git.sops.yaml` at `git_signing.private_key`
- Cosign key pair:
  - public key in `public-keys.yaml` at `cosign.public_key_pem`
  - private key and password in `publish.sops.yaml` at `cosign.private_key` and `cosign.password`
- Homebrew tap deploy SSH key pair:
  - public key in `public-keys.yaml` at `homebrew_tap.public_key_openssh`
  - private key in `publish.sops.yaml` at `homebrew.tap_deploy_key`
- Homebrew tap deploy target repository in `publish.yaml` at `homebrew.tap_deploy_target_repo`
