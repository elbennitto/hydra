# hydra local list

Print the canonical Hydra resource id for each object produced by the same local render pipeline as [`hydra local template`](template.md).

## Synopsis

```text
hydra local list <appId> [appId...] [flags]
```

## Description

Hydra resolves the same app selection, Helm render, scope catalog, `templatePatches`, optional CEL `--include` / `--exclude` filters, and `global.hydra.clones` appendix (when rules exist) as `hydra local template`. Instead of YAML, the command prints **one Hydra id per line**: all ids from every selected app, plus clone-only resources when applicable, **deduplicated** and **sorted lexicographically**.

This is an offline command: no Kubernetes API access is required.

For live cluster inventory ids, use [`hydra gitops list`](../cluster/list.md).

## Arguments

| Argument | Description |
| -------- | ----------- |
| `appId` | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources before listing ids |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources before listing ids |
| `--bootstrap` | | Same clone behavior as `hydra local template` when `global.hydra.clones` rules apply |

Color flags are accepted for consistency with other local commands; id output is plain text (no YAML coloring).

## Examples

```bash
# All rendered resource ids for one app
hydra local list prod.infra.monitoring --hydra-context /path/to/gitops

# Glob select apps, exclude one child app
hydra local list prod.** --exclude-app prod.cluster-infra.cert-manager --hydra-context /path/to/gitops

# Only Deployment ids
hydra local list prod.apps.my-service --include 'kind == "Deployment"' --hydra-context /path/to/gitops
```

## See also

- [`hydra local template`](template.md) — full rendered YAML
- [`hydra local find`](find.md) — CEL projection over rendered resources
- [`hydra gitops list`](../cluster/list.md) — ids from the live cluster view
