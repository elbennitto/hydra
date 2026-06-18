# hydra helm

Run the embedded [Helm](https://helm.sh/) CLI (Helm v4) with the same interface as the standalone `helm` binary.

## Synopsis

```bash
hydra helm [helm arguments and flags]
```

## Description

`hydra helm` runs upstream Helm’s Cobra command tree (`hydra-go/cli/cmd/delegated_cli.go` via `helm.sh/helm/v4/pkg/cmd`) in a separate execution path so Hydra root flags (for example `-v` / `--verbose`) do not collide with Helm’s. All subcommands, flags, plugins, and environment variables match the embedded Helm release.

Hydra skips the usual stderr welcome line for `hydra helm` (and other delegated tool CLIs) so stdout stays suitable for pipes and scripts.

Hydra uses the same Helm library internally for chart rendering, dependency resolution, and `hydra ci run download` / `run test`. `hydra helm` is for ad-hoc chart work (lint, template, repo, install, and so on) without installing a separate `helm` binary.

For subcommands, flags, plugins, and environment variables, use the official upstream documentation: [Helm documentation](https://helm.sh/docs/) (same CLI as `hydra helm`; only the command prefix differs).

## Examples

```bash
# Same as: helm version
hydra helm version

# Lint a chart directory
hydra helm lint ./my-chart

# Render templates locally
hydra helm template my-release ./my-chart -f values.yaml

# Show help for a subcommand
hydra helm template --help
```

## Tutorials

- [Adding Manifests to the Helm Charts](../../tutorials/introduction/02-00-adding-manifests-to-the-helm-charts.md) — compares `helm template` with `hydra local template` during the first local rendering steps

## See also

- [Helm documentation](https://helm.sh/docs/) — upstream CLI reference
- [Delegated tool CLIs](../README.md#delegated-tool-clis) in the command reference
- [`hydra local template`](../local/template.md) — Hydra-native offline render for apps in a Hydra context
