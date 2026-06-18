# hydra local config

Show the effective Hydra configuration for an application under **`global.hydra` from Helm values only** (the merged chart / `values.yaml` path). **Hydra ConfigMap** `data.hydra` fragments are **not** applied here; use [`hydra gitops values`](../cluster/values.md) (or other `hydra gitops *` commands) to see `global.hydra` after ConfigMap merges and `scope` rules.

**`scope` in chart values:** `global.hydra.scope` is **not** allowed in Helm values. If it is set, validation fails. Define `scope` only in a `v1/ConfigMap` with `data.hydra` and annotation `hydra-gitops.org/hydra-config: "true"`.

## Synopsis

```text
hydra local config <appId> [flags]
```

## Description

Output is **one** YAML tree at `global.hydra`:

1. **Helm** — Values from the merged Helm chart (cluster metadata, `refs` from values, `kubectl` allowlists, etc.).
2. **`ownerNamespaces` (implicit)** — Hydra renders **all** non-root apps on the same cluster (offline) and appends any namespace where **only this app** deploys resources to the `ownerNamespaces` list (Kubernetes system namespaces and `kube-*` names excluded). Existing entries from Helm are kept; the result is a sorted union. This mirrors sole-namespace detection used for clone target ownership and avoids editing values when the relationship is already one app per namespace.

Rendering uses the same offline Helm path as other local commands.

## When To Use It

Use `hydra local config` when you need **`global.hydra` exactly as declared in Git/Helm** for this app. Use **`hydra gitops values`** when you need the operator ConfigMap overlays that exist only after the full-cluster template catalog is considered.

## Arguments

| Argument | Description                                        |
| -------- | -------------------------------------------------- |
| `appId`  | A single [App ID](../README.md#app-ids) to inspect |

## Flags

| Flag              | Short | Description                                                                                        |
| ----------------- | ----- | -------------------------------------------------------------------------------------------------- |
| `--hydra-context` |       | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color`         | `-c`  | Force colored YAML output                                                                          |
| `--no-color`      |       | Disable colored output                                                                             |
| `--color-mode`    |       | Color mode: `auto`, `always`, or `never`                                                           |
| `--no-cache`      |       | Disable persistent Helm template cache and in-process Helm-related caches for this run             |

## Examples

```bash
# Helm global.hydra only (no Hydra ConfigMap merge)
hydra local config prod.infra.cert-manager

# With explicit context
hydra local config prod.infra.cert-manager --hydra-context /path/to/context

# Colored YAML (TTY auto-detect; use --color to force)
hydra local config prod.infra.cert-manager --color

# Inspect nested keys
hydra local config prod.infra.cert-manager | yq '.global.hydra.kubectl'
```

## See Also

- [Hydra ConfigMaps](../../appendix/concepts/hydra-configmaps.md) — how ConfigMaps are merged from Git (not from the API on apply/uninstall)
- [`hydra local values`](values.md) — show all merged Helm values for the app (Helm-only `global.hydra`)
- [`hydra gitops values`](../cluster/values.md) — Helm values with `global.hydra` merged using Hydra ConfigMaps from every app on the cluster (includes `scope` filtering)
- [`hydra local template`](template.md) — see rendered manifests (where Hydra ConfigMaps are defined)
