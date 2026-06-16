# Release and Signing

This repository uses a workflow-first release flow with inline CI logic.

## CI entrypoints

GitHub Actions implement and orchestrate these pipelines directly:

- Main pipeline: [.github/workflows/release.yml](.github/workflows/release.yml)
- Tag pipeline: [.github/workflows/publish.yml](.github/workflows/publish.yml)
- Renovate pipeline: [.github/workflows/renovate.yml](.github/workflows/renovate.yml)

Local developer helper script:

- Local container build: [scripts/build-container-image.sh](scripts/build-container-image.sh)

The tag publish pipeline runs in a pinned container image:

- ghcr.io/catthehacker/ubuntu:full-24.04@sha256:0530f6204841c66f3da8d6817cc0637aff66a12f20fb3cc9e64a9c7a5dff6706

## Versioning flow

1. Push to main triggers [.github/workflows/release.yml](.github/workflows/release.yml).
2. The release workflow runs `semantic-release` on `main`.
3. During the semantic-release `prepare` step, `CHANGELOG.md` and the root `README.md` are regenerated and committed by the release identity.
4. `semantic-release` creates a lightweight release tag on that signed release commit first so its git notes land on the target commit.
5. Right before the tag push, the CI wrapper rewrites only newly created release tags as signed annotated tags and then pushes them.
6. Tag push triggers [.github/workflows/publish.yml](.github/workflows/publish.yml).

Unsigned or invalidly signed tags fail in the publish workflow before release publishing.

The lightweight-then-sign flow is intentional: current `semantic-release` releases still read channel metadata from git notes in a way that works reliably when the note remains attached to the commit, but becomes noisy or brittle with directly created signed tags.

## Published artifacts

### CLI archives

- Produced by [hydra-go/.goreleaser.yml](hydra-go/.goreleaser.yml)
- Embedded version comes from build ldflags
- A `checksums.txt` file is published for downloaded binaries
- Signed in release pipeline

### Container image

- Built from [tools/build-container-image/Dockerfile](tools/build-container-image/Dockerfile)
- Runtime stage is scratch and contains only the hydra binary
- Multi-platform image is published and signed by digest

### Homebrew

- Formula and cask publishing are handled via GoReleaser
- `hydra` is published as a Homebrew formula that builds the latest release from source
- `hydra-bin` is published as a Homebrew cask that installs the GitHub release binary
- Published to `hydra-gitops/homebrew-tap`

## Supported platforms

### CLI archives

- linux/amd64
- linux/arm64
- darwin/amd64
- darwin/arm64

### Docker image

- linux/amd64
- linux/arm64

### Homebrew

- macOS
- Linux

## Secrets and keys

Public keys are stored in:

- [.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml](.github/secrets/repos/hydra-gitops/hydra/public-keys.yaml)

Encrypted secret files:

- [.github/secrets/repos/hydra-gitops/hydra/git.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/git.sops.yaml)
- [.github/secrets/repos/hydra-gitops/hydra/publish.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/publish.sops.yaml)
- [.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml)
- [.github/secrets/repos/hydra-gitops/hydra/age-pipeline-keys.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/age-pipeline-keys.sops.yaml)

All encrypted files are decryptable with the owner SSH key configured in [.sops.yaml](.sops.yaml).

Required GitHub secrets:

- SOPS_AGE_KEY_RELEASE
- SOPS_AGE_KEY_PUBLISH
- GITHUB_TOKEN (provided by GitHub Actions)
- RENOVATE_TOKEN (required PAT or GitHub App token for dependency update PRs)

Renovate token source:

- Store the encrypted source value in [.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml](.github/secrets/repos/hydra-gitops/hydra/renovate.sops.yaml) under `renovate.token`.
- Create the token in `GitHub -> Settings -> Developer settings -> Personal access tokens -> Fine-grained tokens -> Generate new token`.
- Use `Resource owner: hydra-gitops`.
- Use `Repository access: Only select repositories` and select `hydra`.
- Grant repository permissions `Contents: Read and write` and `Pull requests: Read and write`.
- Grant `Issues: Read and write` as well if Renovate should manage its dashboard issue or leave issue comments.
- If the organization requires approval for fine-grained tokens, an org owner must approve the token before it can access private repositories.
- Upload the decrypted value to the repository secret with `repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq '.nameWithOwner')}"; secrets_dir=".github/secrets/repos/${repo}"; sops --decrypt --extract '["renovate"]["token"]' "${secrets_dir}/renovate.sops.yaml" | gh secret set RENOVATE_TOKEN --repo "${repo}"`.

Homebrew cask publishing writes to `hydra-gitops/homebrew-tap`.
Use a separate write-enabled GitHub token for that repository.
