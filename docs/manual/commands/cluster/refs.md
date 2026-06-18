# hydra gitops refs

Print **transitive** reference reachability for a specific Hydra resource id, using **locally rendered templates merged with live cluster** data—the same ref source model as [`hydra gitops inspect`](inspect.md).

## Usage

```text
hydra gitops refs <cluster> <id> [flags]
```

## Arguments

| Argument | Description |
| -------- | ----------- |
| `cluster` | Cluster name (same segment as in app ids; must not contain `.`). |
| `id` | Canonical Hydra resource id: `group/version/kind/namespace/name`, or for core API groups `version/kind/namespace/name`. May refer to objects that exist only on the **live** cluster (for example operator-created workloads), not only to ids from templates. |

## Behavior

Hydra renders the named cluster’s applications, connects to the Kubernetes API, and computes refs **twice** (templates and live inventory), then **merges** and deduplicates edges—matching [`hydra gitops inspect`](inspect.md). From that merged graph it runs **breadth-first expansion** from `<id>` for **incoming** and **outgoing** directions, **up to 10 hop levels** per direction, aligned with the Hydra UI graph.

**Distance** uses the same convention as [`hydra local refs`](../local/refs.md) and the UI:

| Distance | Meaning |
| -------- | ------- |
| `0` | The **current** (anchor) entity, `<id>`. |
| Positive | **Outgoing** hop count from the anchor along `From → To` edges. |
| Negative | **Incoming** hop count when traversing toward the anchor. |

Transitive expansion is **always on**; there is **no** flag to restrict output to direct neighbors.

Stdout is a **YAML sequence of entity-centered rows** (reachable id, signed distance, direction, relation/label metadata). An empty result prints as an empty YAML sequence.

Before using this command in production, validate the kubeconfig context with [`hydra gitops validate-current-context`](validate-current-context.md).

## Flags

| Flag | Description |
| ---- | ----------- |
| `--hydra-context` | Path to the Hydra context directory (or `HYDRA_CONTEXT`). |
| `--helm-network-mode` | How Helm resolves charts (`online`, `local`, `offline`, `error`). |
| `--no-cache` | Disable persistent Helm template cache and in-process Helm-related caches for this run. |
| `--color` | Colorize YAML output when supported. |
| `--bootstrap` | Include `global.hydra.clones` rules tagged `bootstrap` when materializing clone predictions for CEL options (same meaning as `hydra gitops inspect`). |

## Examples

```bash
hydra gitops validate-current-context prod

# Transitive refs with merged template + live graph
hydra gitops refs prod apps/v1/Deployment/my-ns/my-app

hydra gitops refs prod v1/ConfigMap/my-ns/my-cm --helm-network-mode local
```

## See also

- [`hydra local refs`](../local/refs.md) — transitive listing from **template-only** data
- [`hydra gitops inspect`](inspect.md) — interactive TUI with **Dist** column, filter popup, and the same merged graph
- [`hydra gitops review`](review.md) — validate reference integrity against the live cluster
- [`hydra gitops validate-current-context`](validate-current-context.md) — confirm kubeconfig context
- [Hydra CLI overview](../README.md)
