# Hydra

[![Latest release](https://img.shields.io/github/v/release/hydra-gitops/hydra?sort=semver)](https://github.com/hydra-gitops/hydra/releases/tag/v1.2.0)
[![release](https://github.com/hydra-gitops/hydra/actions/workflows/release.yml/badge.svg)](https://github.com/hydra-gitops/hydra/actions/workflows/release.yml)
![Container image](https://img.shields.io/badge/container-ghcr.io-blue)

Hydra provides a standardized GitOps workflow for Helm and Argo CD with a CLI-first toolchain and reproducible release pipelines.

Latest signed release: [v1.2.0](https://github.com/hydra-gitops/hydra/releases/tag/v1.2.0)

## Install

### Homebrew

Tap the Hydra repository first:

```bash
brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap
```

macOS recommended (build the latest released version from source):

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Linux recommended (download the latest released binary from GitHub releases):

```bash
brew trust --formula hydra-gitops/tap/hydra-bin
brew install hydra-gitops/tap/hydra-bin
```

Linux can also self-compile from source if preferred:

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Hydra provides both Homebrew artifacts:

- `hydra-bin` downloads the prebuilt CLI from GitHub releases and is recommended on Linux.
- `hydra` builds from source and is recommended on macOS.

If you install the GitHub-downloaded binary on macOS, Gatekeeper may block the first launch with:

> `"hydra" Not Opened. Apple could not verify "hydra" is free of malware that may harm your Mac or compromise your privacy.`

You can allow an exception in `System Settings > Privacy & Security`, or avoid the warning by using the source formula:

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Uninstall Homebrew packages with:

```bash
brew uninstall hydra-gitops/homebrew-tap/hydra
# or
brew uninstall hydra-gitops/homebrew-tap/hydra-bin
brew untap hydra-gitops/tap
```

### Docker (linux/amd64 and linux/arm64)

```bash
docker pull ghcr.io/hydra-gitops/hydra:latest
docker run --rm ghcr.io/hydra-gitops/hydra:latest --help
```

Stable releases also publish `vX.Y.Z`, `vX.Y`, `vX`, and `latest` tags.

### Download CLI archives

Release assets are published on each signed version tag:

- https://github.com/hydra-gitops/hydra/releases/tag/v1.2.0

Verify downloaded archives with the published checksum file:

```bash
curl -LO https://github.com/hydra-gitops/hydra/releases/download/v1.2.0/checksums.txt
shasum -a 256 --check checksums.txt
```

## Verify releases

Public keys are published in [.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml](.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml).

- Release tags are created as lightweight tags first and are rewritten to signed annotated tags immediately before push so `semantic-release` can keep its git-note metadata on the tagged commit.
- Release tag signatures are verified before release jobs start.
- Downloaded CLI archives can be checked against `checksums.txt`.
- CLI archives are signed during the release workflow.
- Published container images are signed by digest.

## CI tool installation

GitHub Actions installs `cosign`, `sops`, and `goreleaser` into `$HOME/.cosign`.
The `sigstore/cosign-installer` action adds that directory to `GITHUB_PATH`, so later
workflow steps can call these binaries directly without `sudo` or `/usr/local/bin`.

## CI secrets

Renovate requires a dedicated GitHub token that can open pull requests. Keep the source
value in [.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml) under
`renovate.token`, then upload it to GitHub with:

```bash
repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq '.nameWithOwner')}"
secrets_dir=".github/secrets/repos/${repo}"
sops --decrypt --extract '["renovate"]["token"]' "${secrets_dir}/renovate.sops.yaml" | gh secret set RENOVATE_TOKEN --repo "${repo}"
```

Create the token in the GitHub UI as a fine-grained personal access token:

1. Open `GitHub -> Settings -> Developer settings -> Personal access tokens -> Fine-grained tokens -> Generate new token`.
2. Set `Resource owner` to `hydra-gitops`.
3. Set `Repository access` to `Only select repositories` and choose `hydra`.
4. Under repository permissions, grant `Contents: Read and write` and `Pull requests: Read and write`.
5. If you want Renovate to update its dashboard issue or leave issue comments, also grant `Issues: Read and write`.
6. Create the token, copy it once, and replace the dummy value in `.github/secrets/repos/<owner>/<repo>/renovate.sops.yaml`.

If the organization requires approval for fine-grained tokens, the token stays pending until an org owner approves it.

## Build locally

```bash
./scripts/build-container-image.sh hydra:test v0.0.0-local
```

Build release archives from the repo root with:

```bash
(
  cd hydra-go
  goreleaser release --clean --snapshot --config .goreleaser.yml
)
```

## Developer scripts

- Local container build: [scripts/build-container-image.sh](scripts/build-container-image.sh)
- Markdown linting: [scripts/lint-markdown-docs.sh](scripts/lint-markdown-docs.sh)
- Root README generation: [scripts/generate-readme.sh](scripts/generate-readme.sh)

## Documentation

- User and manual docs: [docs/](docs/)
- Go release config: [hydra-go/.goreleaser.yml](hydra-go/.goreleaser.yml)
- Release changelog: [CHANGELOG.md](CHANGELOG.md)
- Release process and platform matrix: [RELEASE.md](RELEASE.md)
- Renovate configuration: [renovate.json](renovate.json)
