# hydra gitops values

Display merged Helm values for one application, with **`global.hydra`** merged from Helm **and** Hydra ConfigMap `data.hydra` documents discovered across **all** apps on the same cluster (full-cluster Helm render catalog).

## Synopsis

```text
hydra gitops values <appId> [flags]
```

## Description

Hydra loads the same layered Helm values as [`hydra local values`](../local/values.md) (`LoadValuesMap`), then replaces the **`global.hydra`** subtree with the effective map produced by:

1. A **full-cluster** `helm template` render (all apps on the cluster, respecting the same skip-root-app rules as other cluster render paths) to collect chart-owned Hydra ConfigMaps.
2. **Helm + ConfigMap** deep-merge (`MergeHelmHydraWithConfigMapDocuments`) over the cluster-wide Hydra ConfigMap catalog (`PartitionHydraConfigDocumentsByApp`), then **per-app scope filtering**: optional `scope` list in each `data.hydra` document (`mode: include|exclude`, `values`: app-id globs matching CLI patterns such as `prod.**`; see [`hydra local config`](../local/config.md) vs cluster docs). **`global.hydra.scope` is invalid in Helm chart values** — validation fails; use ConfigMaps only.
3. **`MergeInferredOwnerNamespacesIntoHydraMap`** so sole-namespace ownership hints match the full-cluster render.

All other top-level and chart-specific keys are unchanged from `hydra local values`; only **`global.hydra`** reflects ConfigMap overlays from the whole cluster catalog.

The effective `global.hydra` subtree and the final chart input map are produced by **`PrepareClusterHelmMergedHydraMaps`** and **`ClusterHelmInputValuesMap`** in `hydra-go` — the same helpers [`hydra gitops template`](template.md) uses when wiring values into its second Helm pass, so template rendering and the printed values stay aligned.

No Kubernetes API connection is required (same as local rendering). The `hydra gitops` parent group is used because the merge scope is **cluster-wide** ConfigMap participation, not a single-app render catalog.

## Differences from `hydra local values`

| Topic | `hydra local values` | `hydra gitops values` |
| ----- | -------------------- | ----------------------- |
| `global.hydra` in output | From Helm / values files only | Helm `global.hydra` merged with **all** Hydra ConfigMap `data.hydra` fragments visible in the full-cluster template render (each CM may declare `scope`) |
| Other value keys | Full merged Helm values | Same as local (unchanged except `global.hydra`) |

Use **`hydra local values`** when you only care about Git/Helm inputs. Use **`hydra gitops values`** when ConfigMaps rendered by **other** apps on the cluster contribute to `global.hydra` for this app.

## Arguments

| Argument | Description                                        |
| -------- | -------------------------------------------------- |
| `appId`  | A single [App ID](../README.md#app-ids) to inspect |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

Parent command `hydra gitops` also exposes `--qps` and `--api-burst` for Kubernetes client throttling; they are unused by this read-only render-only command.

## Examples

```bash
hydra gitops values prod.infra.cert-manager --hydra-context /path/to/context

hydra gitops values prod.apps.my-service --helm-network-mode offline | yq '.global.hydra'
```

## See Also

- [Hydra ConfigMaps](../../appendix/concepts/hydra-configmaps.md) — how ConfigMaps are merged from the full-cluster template catalog (not read from the cluster for writes)
- [`hydra local values`](../local/values.md) — Helm-only merged values
- [`hydra local config`](../local/config.md) — print Helm **`global.hydra` only** (no Hydra ConfigMap merge)
- [`hydra gitops template`](template.md) — full manifests with cluster-wide `templatePatches` and API normalization
