# hydra gitops inspect

Interactively browse the reference graph for a Hydra resource using
locally rendered templates combined with a live cluster connection.

Hydra loads rendered apps and live cluster inventory **once** per command
invocation (including when you use the id picker without `<id>`). The TUI then
navigates the reference graph in memory without reloading the cluster.

The picker status rows now come from the same resource model rows used by other cluster commands: one normalized resource ID can carry both template presence and live presence, so picker rows classify IDs directly as `ok`, `template only`, or `cluster only` from that shared model.

## Synopsis

```text
hydra gitops inspect <cluster> [id] [flags]
```

## Data Source

`hydra gitops inspect` renders the named cluster's applications locally
and uses the live Kubernetes API for additional context during ref
resolution (ConfigMap parsers, clone materialization, CEL options).

Hydra still builds the merged inspect reference graph from the shared inventory snapshot, but the template/live status shown in the picker is now read from the resource model instead of two independently queried collections.

The reference list uses **transitive** breadth-first expansion (**max 10**
levels per direction) on the **merged** template+live graph and shows
**`Dist`** as the **first** list column. Unlike local inspect, cluster inspect also shows
**Status** in the picker, header, and list; see the
[shared inspect documentation](../inspect-shared.md).

The optional `<id>` may refer to any resource Hydra loads from the cluster
(for example a live `Pod` created by an operator), not only objects that
appear in rendered templates.

Before running this command, validate the kubeconfig context with
[`hydra gitops validate-current-context`](validate-current-context.md).

For a purely offline variant that does not require cluster connectivity, use
[`hydra local inspect`](../local/inspect.md).

## Shared Behavior

The interactive TUI layout, keyboard controls, filter popup, and navigation behavior are
identical to `hydra local inspect`. See the
[shared inspect documentation](../inspect-shared.md) (including
[Detail panel](../inspect-shared.md#detail-panel)) for layout and keys.

## Arguments

| Argument | Description |
| -------- | ----------- |
| `cluster` | Cluster name (same segment as in app ids; must not contain `.`). |
| `id` | Optional. Canonical Hydra resource id. If omitted, Hydra shows a picker over template and live ids (see [shared inspect docs](../inspect-shared.md#id-picker)). |

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--bootstrap` | | Include `global.hydra.clones` rules tagged `bootstrap` when materializing clone predictions for CEL options |

## Examples

```bash
# Validate cluster first
hydra gitops validate-current-context prod

# Picker (templates + live cluster), then graph
hydra gitops inspect prod

# Browse refs for a Deployment
hydra gitops inspect prod apps/v1/Deployment/my-ns/my-app
```

## See Also

- [Shared inspect documentation](../inspect-shared.md) - full TUI reference
- [`hydra local inspect`](../local/inspect.md) - same TUI with offline data
- [`hydra gitops refs`](refs.md) - print transitive reference reachability as YAML (same merged graph)
- [`hydra local refs`](../local/refs.md) - YAML listing from templates only
- [`hydra gitops review`](review.md) - validate reference integrity against live cluster
- [`hydra gitops validate-current-context`](validate-current-context.md) - confirm kubeconfig context
