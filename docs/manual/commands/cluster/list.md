# hydra gitops list

Print Hydra resource ids for objects visible in a live cluster.

## Synopsis

```text
hydra gitops list <cluster> [flags]
```

## Description

Connects to the Kubernetes API and lists the [Hydra id](../../../develop/shared/details/hydra-yaml.md) of each resource that matches Hydra's cluster view, one id per line. Output is plain text (not YAML), suitable for scripting and `xargs`.

The live cluster reader now comes from the shared resource model used by other `hydra gitops` commands. Hydra still lists the live API snapshot, but `cluster list` reads normalized per-ID inventory entities instead of maintaining a command-local live-only view.

Filtering uses the same CEL expressions as [`hydra gitops dump`](dump.md).

With `--skip-owner-refs`, Hydra reads the **entire** live side of that resource model first, applies your `--include` / `--exclude` predicates to that set, then removes any object that has a `metadata.ownerReference` with a non-empty UID that matches **another live object** in that inventory. Child objects such as ReplicaSets or Pods under a Deployment are therefore omitted; dangling owner UIDs (no matching live object) do not cause removal.

## Arguments

| Argument  | Description                                        |
| --------- | -------------------------------------------------- |
| `cluster` | The cluster name (as defined in the Hydra context) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--skip-owner-refs` | | List the full cluster inventory, apply CEL filters, then omit objects whose `metadata.ownerReferences` point at another live object by UID |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# All ids in a cluster
hydra gitops list prod

# Only Deployments
hydra gitops list prod --include 'kind == "Deployment"'

# Exclude a namespace
hydra gitops list prod --exclude 'namespace == "kube-system"'

# Only ownership roots (omit ReplicaSets, Pods, etc. owned by another listed object)
hydra gitops list prod --skip-owner-refs
```

## See Also

- [`hydra gitops dump`](dump.md) — full multi-document YAML for the same resource set
