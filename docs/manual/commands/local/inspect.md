# hydra local inspect

Interactively browse the reference graph for a Hydra resource using
locally rendered templates.

## Synopsis

```text
hydra local inspect <cluster> [id] [flags]
```

## Data Source

`hydra local inspect` renders **all effectively enabled applications** on the
named cluster from the Hydra context directory and computes references
offline. It never connects to a live Kubernetes cluster.

The reference list uses **transitive** breadth-first expansion (**max 10**
levels per direction) and a **`Dist`** column as the **first** list column. Local inspect
does **not** show **Status** in the picker, header, or list; see the
[shared inspect documentation](../inspect-shared.md).

For the same interactive TUI against a **live cluster**, see
[`hydra gitops inspect`](../cluster/inspect.md).

## Shared Behavior

The interactive TUI layout, keyboard controls, filter popup, and navigation behavior are the
same as for `hydra gitops inspect`. See the
[shared inspect documentation](../inspect-shared.md) (including
[Detail panel](../inspect-shared.md#detail-panel)) for layout and keys.

## Arguments

| Argument | Description |
| -------- | ----------- |
| `cluster` | Cluster name (same segment as in app ids; must not contain `.`). |
| `id` | Optional. Canonical Hydra resource id. If omitted, Hydra shows a picker over template-known ids (see [shared inspect docs](../inspect-shared.md#id-picker)). |

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Picker: press `/`, filter, then inspect the graph
hydra local inspect prod

# Browse refs for a Deployment on cluster prod
hydra local inspect prod apps/v1/Deployment/my-ns/my-app

# Offline chart resolution
hydra local inspect prod v1/ConfigMap/my-ns/my-cm --helm-network-mode offline
```

## See Also

- [Shared inspect documentation](../inspect-shared.md) - full TUI reference
- [`hydra gitops inspect`](../cluster/inspect.md) - same TUI with live cluster data
- [`hydra local refs`](refs.md) - print transitive reference reachability as YAML
- [`hydra gitops refs`](../cluster/refs.md) - YAML listing on the merged template+cluster graph
- [`hydra local review`](review.md) - validate reference integrity offline
