# Installation

## Prerequisites

- **Go** (1.21+) — Required to build the CLI
- **kubectl** — Kubernetes CLI, configured with cluster access
- **Helm** (3.x) — Used internally for template rendering
- **ArgoCD** — Deployed on the target cluster (for GitOps workflows)

## Install With Homebrew

Add the Hydra tap:

```bash
brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap
```

Install the package for your platform:

macOS recommended (source formula):

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Linux recommended (download the released binary from GitHub):

```bash
brew trust --cask hydra-gitops/tap/hydra-bin
brew install --cask hydra-gitops/tap/hydra-bin
```

Hydra ships both artifacts via Homebrew:

- `hydra-bin` downloads prebuilt CLI archives from signed GitHub releases and is recommended on Linux.
- `hydra` builds from source tarballs in the tap formula and is recommended on macOS.

If you prefer a source build on Linux, this also works:

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Uninstall Homebrew packages with:

```bash
brew uninstall hydra-gitops/tap/hydra
# or
brew uninstall --cask hydra-gitops/tap/hydra-bin
brew untap hydra-gitops/tap
```

## Building the Hydra CLI

```bash
cd hydra-go
go build -o hydra ./cli
```

Move the binary to a directory in your `$PATH`:

```bash
mv hydra /usr/local/bin/
hydra version
```

## Setting Up HYDRA_CONTEXT

Hydra needs to know where your cluster configurations live. Set the `HYDRA_CONTEXT` environment variable to point at the context directory inside your GitOps repository:

```bash
export HYDRA_CONTEXT=/path/to/gitops-repository/clusters/development
```

Each subdirectory in this path represents one cluster (e.g., `dev`, `cicd`, `team1`, `team2`).

See [Configuration → HYDRA_CONTEXT](../configuration/hydra-context.md) for details.

## Configuring Kubeconfig Mapping

If you manage multiple clusters, create `~/.config/hydra/config.yaml` to map cluster directories to specific kubeconfig files:

```yaml
contexts:
  - path: /path/to/gitops-repository/clusters/prod
    config: ~/.kube/prod.conf
    name: prod-admin
  - path: /path/to/gitops-repository/clusters/test
    config: ~/.kube/test.conf
    name: test-admin
```

See [Configuration → config.yaml](../configuration/config-yaml.md) for the full reference.

## Verifying Connectivity

```bash
hydra gitops validate-current-context prod
```

This verifies that your current kubectl context matches the allowed contexts configured for the cluster. If successful, Hydra can communicate with the cluster API.

## Next Steps

- [Tutorial Introduction](../tutorials/introduction/) — Start with Hydra modes, `HYDRA_CONTEXT`, and the first local workflow
- [Concepts: Context and Clusters](../concepts/context-and-clusters.md) — Understand the context model
