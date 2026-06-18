# hydra gitops status

Check per app whether rendered desired state is in sync with live cluster resources.

## Synopsis

```text
hydra gitops status <appId> [appId...] [flags]
```

## Description

`hydra gitops status` is the cluster-based status command for Hydra-managed applications.

It answers a different question than [`hydra argocd status`](../argocd/README.md):

- `hydra argocd status` shows the real sync status currently reported by ArgoCD.
- `hydra gitops status` computes a per-app in-sync check from Hydra's rendered desired state versus the live cluster resources tracked for the selected apps.

The live cluster side is read from the same resource model rows used by other cluster commands. Hydra still renders desired state separately, but the cluster reader itself now consumes the shared normalized per-ID inventory model.

Use this command when you want to know whether an app is currently in sync on the cluster from Hydra's point of view, even if ArgoCD has not reconciled yet or reports additional controller-specific details.

## Arguments

| Argument | Description |
| -------- | ----------- |
| `appId` | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

When you target multiple apps, use `--exclude-app` to subtract known exceptions from the resolved selection.

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## What Gets Checked

- Hydra resolves the selected app IDs, then subtracts any repeated `--exclude-app` patterns.
- Hydra renders the desired resources for each selected app.
- Hydra compares those desired resources with the tracked live resources on the selected cluster, read from the shared resource model.
- The command reports an in-sync or out-of-sync result per app.

When you need the detailed differences behind an out-of-sync result, continue with [`hydra gitops diff`](diff.md).

## Examples

```bash
# Check one app
hydra gitops status prod.infra.cert-manager

# Check all child apps below one root app
hydra gitops status prod.infra.*

# Check all prod apps except one excluded app
hydra gitops status prod.** --exclude-app prod.infra.argocd

# Use offline Helm dependency handling during status calculation
hydra gitops status prod.apps.* --helm-network-mode offline
```

## See Also

- [`hydra argocd`](../argocd/README.md) - ArgoCD-reported app status and sync control
- [`hydra gitops diff`](diff.md) - inspect the detailed differences behind an out-of-sync result
- [`hydra gitops review`](review.md) - validate rendered references against live cluster targets
