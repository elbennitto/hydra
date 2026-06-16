# Hydra

![Latest release]({{LATEST_RELEASE_BADGE}})
![Container image]({{CONTAINER_BADGE}})

Hydra provides a standardized GitOps workflow for Helm and Argo CD with a CLI-first toolchain and reproducible release pipelines.

## Install

### Homebrew (macOS and Linux)

```bash
brew install hydra-gitops/hydra/hydra
```

### Docker (linux/amd64 and linux/arm64)

```bash
docker pull {{IMAGE}}:latest
docker run --rm {{IMAGE}}:latest --help
```

### Download CLI archives

Release assets are published on each signed version tag:

- https://github.com/{{REPO}}/releases/latest

Verify downloaded archives with the published checksum file:

```bash
curl -LO https://github.com/{{REPO}}/releases/latest/download/checksums.txt
shasum -a 256 --check checksums.txt
```

## Verify releases

Public keys are published in [.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml](.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml).

- Release tag signatures are verified before release jobs start.
- Downloaded CLI archives can be checked against `checksums.txt`.
- CLI archives are signed during the release workflow.
- Published container images are signed by digest.

## Build locally

```bash
./scripts/build-container.sh hydra:test v0.0.0-local
```

## Developer scripts

- Local container build: [scripts/build-container.sh](scripts/build-container.sh)
- Markdown linting: [scripts/lint-markdown-docs.sh](scripts/lint-markdown-docs.sh)

## Documentation

- User and manual docs: [docs/](docs/)
- Release process and platform matrix: [RELEASE.md](RELEASE.md)
- Renovate configuration: [renovate.json](renovate.json)
