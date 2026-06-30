# hydra local test

Run tests for Hydra applications.

## Synopsis

```text
hydra local test <subcommand> [flags]
```

## Description

Provides subcommands for testing Hydra application configurations. Currently supports reference parser validation.

## Subcommands

### hydra local test refs

Validate that the ref-parser correctly resolves references in the rendered output. Compares the current output against stored expected files to detect regressions.

```text
hydra local test refs <appId> [appId...] [flags]
```

| Flag | Description |
| --- | --- |
| `--update` | Regenerate expected files from current output (use after intentional changes) |
| `--hydra-context` | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--exclude-app` | Glob pattern to exclude applications (repeatable) |
| `--helm-network-mode` | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Run ref tests for a single app
hydra local test refs prod.infra.cert-manager

# Run ref tests for all apps on a cluster
hydra local test refs prod.**

# Update expected files after intentional changes
hydra local test refs prod.** --update

# Test multiple specific apps
hydra local test refs prod.infra.cert-manager prod.apps.my-service
```

## See Also

- [`hydra ci run test`](../ci/README.md) — chart-level CI tests (lint, template validation)
- [`hydra local template`](template.md) — manually inspect rendered output
