# hydra argocd sync

Manage ArgoCD sync for apps through `AppProject` resources.

## Synopsis

```bash
hydra argocd sync <auto|manual|prevent> <appId> [appId...] [flags]
```

## Description

`hydra argocd sync` is the canonical command family for controlling whether ArgoCD may reconcile the selected applications. It does **not** trigger an immediate reconciliation; it updates the sync configuration on the relevant ArgoCD `AppProject` resources. Because it is a mutating command, at least one app ID is required.

| Subcommand | Result |
| ---------- | ------ |
| `auto` | Automatic reconciliation is allowed (green in the ArgoCD UI) |
| `manual` | Drift stays visible, but reconciliation requires manual action (yellow) |
| `prevent` | Automatic and manual sync are both blocked (red) |

The same modes are also available under [`hydra gitops sync`](../cluster/sync.md), which additionally offers `status` for a read-only view.

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--dry-run` | `-d` | Simulate the change without mutating ArgoCD resources |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Freeze reconciliation for a maintenance window
hydra argocd sync prevent prod.cluster-infra.ingress-nginx

# Re-enable automatic reconciliation afterward
hydra argocd sync auto 'prod.demo.*'

# Switch a selected set to manual sync, except one known outlier
hydra argocd sync manual prod.** --exclude-app prod.infra.argocd

# Preview the sync change only
hydra argocd sync prevent prod.infra.* --dry-run
```

## See Also

- [hydra gitops sync](../cluster/sync.md) — control sync modes (`status`, `auto`, `manual`, `prevent`)
- [hydra argocd status](status.md) — read the ArgoCD-reported sync state
