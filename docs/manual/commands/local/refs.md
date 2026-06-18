# hydra local refs

Print **transitive** reference reachability for a specific Hydra resource id on a cluster, using **offline** Helm-rendered templates only.

## Usage

```text
hydra local refs <cluster> <id> [flags]
```

## Arguments

| Argument | Description |
| -------- | ----------- |
| `cluster` | Cluster name (same segment as in app ids; must not contain `.`). |
| `id` | Canonical Hydra resource id: `group/version/kind/namespace/name`, or for core API groups `version/kind/namespace/name`. |

## Behavior

Hydra renders **all effectively enabled** applications on the named cluster, extracts references the same way as review (including explicit Secret/ConfigMap key attributes), and builds the directed reference graph from the resulting `Ref` edges.

The command then performs a **breadth-first expansion** from `<id>` **separately** for **incoming** and **outgoing** directions, **up to 10 hop levels** per direction—the same cap as the Hydra UI dependency graph.

**Distance** follows the Hydra UI convention:

| Distance | Meaning |
| -------- | ------- |
| `0` | The **current** (anchor) entity, `<id>`. |
| Positive | **Outgoing**: number of hops along `From → To` edges away from the anchor. |
| Negative | **Incoming**: number of hops **toward** the anchor when traversing edges in reverse. |

Transitive expansion is **always on**; there is **no** flag to fall back to direct edges only.

Output is a **YAML sequence of entity-centered rows**: each element describes **one reachable resource id** with **signed distance**, **direction** (incoming vs outgoing relative to the anchor), and **merged relation / label** metadata derived from the ref graph—not a raw list limited to direct `Ref` records whose `from` or `to` equals `<id>`. An empty result prints as an empty YAML sequence.

## Flags

| Flag | Description |
| ---- | ----------- |
| `--hydra-context` | Path to the Hydra context directory (or `HYDRA_CONTEXT`). |
| `--helm-network-mode` | How Helm resolves charts (`online`, `local`, `offline`, `error`). |
| `--no-cache` | Disable persistent Helm template cache and in-process Helm-related caches for this run. |
| `--color` | Colorize YAML output when supported. |

## Examples

```bash
# Transitive refs for a ConfigMap (templates only), up to 10 hops each way
hydra local refs prod v1/ConfigMap/my-ns/my-cm --hydra-context /path/to/gitops

# Offline chart resolution
hydra local refs prod apps/v1/Deployment/my-ns/my-deploy --helm-network-mode local
```

## See also

- [`hydra gitops refs`](../cluster/refs.md) — same transitive listing with templates **plus** live cluster merge
- [`hydra local inspect`](inspect.md) — interactive TUI with **Dist** column, filter popup, and the same distance rules
- [`hydra local review`](review.md) — validate references across selected apps
- [Hydra CLI overview](../README.md)
