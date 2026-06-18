# hydra argocd

Read ArgoCD application status and manage ArgoCD sync.

## Synopsis

```text
hydra argocd <status|sync> [flags]
```

## Description

`hydra argocd` is the top-level command family for ArgoCD-facing operations in Hydra.

- `hydra argocd status` is the read-only status command. It shows the real sync state currently reported by ArgoCD for the selected applications.
- `hydra argocd sync` is the canonical sync command family. It changes whether ArgoCD may reconcile the selected applications automatically, manually, or not at all.

Use [`hydra gitops status`](../cluster/status.md) when you want a cluster-based per-app in-sync check derived from rendered desired state versus live cluster resources. Use `hydra argocd status` when you want ArgoCD's own view.

## App Selection

`hydra argocd status` and `hydra argocd sync` use the normal [App ID](../README.md#app-ids) and [wildcard](../README.md#wildcards) syntax, but they differ in whether app IDs are optional:

- `hydra argocd status` accepts zero, one, or more app IDs. Without app IDs, it shows the ArgoCD view for all visible applications on the selected cluster.
- `hydra argocd sync <auto|manual|prevent>` is mutating and therefore requires at least one app ID.

When you select multiple apps, `--exclude-app` documents and narrows the effective target set:

- Include patterns build the initial app set.
- Repeated `--exclude-app` patterns subtract apps from that set.
- With `hydra argocd status`, `--exclude-app` may also be used together with the implicit "all applications" view.

## Subcommands

### hydra argocd status

Show the real application sync state from ArgoCD.

```text
hydra argocd status [appId...] [flags]
```

This command is read-only. It talks to ArgoCD and reports what ArgoCD currently considers the application's sync state, rather than computing drift from Hydra's local render pipeline.

Output is grouped by root app and formatted as aligned columns (**State**, **Mode**, **Operation**). When **`status.operationState`** is set, the **Operation** column shows the phase and duration (**elapsed** while `phase` is `Running`, or **last run** when `startedAt` and `finishedAt` are set). If there is no operation state, the column shows **`none`** in gray.

When **`status.conditions`** is non-empty (**App conditions** in the ArgoCD UI), Hydra prints a summary and each condition under that application row, indented; long lines use the **same global width across all aligned status tables in the current command output** (through the end of the **Operation** column). **First**, on each segment at least that long, replace the **first** colon-space pair **at or after** that width with a line break after the colon; keep the left part as-is and apply the same rule only to the **right** remainder. **Then** word-wrap any segment that still exceeds the width.

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--color` | `-c` | Force colored output |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

### hydra argocd sync

Manage sync for the selected applications through ArgoCD `AppProject` resources.

```text
hydra argocd sync <auto|manual|prevent> <appId> [appId...] [flags]
```

Available modes:

| Mode | Result |
| ---- | ------ |
| `auto` | Automatic reconciliation is allowed |
| `manual` | Drift is visible, but reconciliation requires manual action |
| `prevent` | Automatic and manual sync are both blocked |

#### Common Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--color` | `-c` | Force colored output |
| `--dry-run` | `-d` | Simulate the change without mutating ArgoCD resources |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Show the ArgoCD-reported sync state for all applications
hydra argocd status

# Show ArgoCD status for selected apps only
hydra argocd status prod.infra.cert-manager prod.apps.api

# Show all prod apps except one excluded child app
hydra argocd status prod.** --exclude-app prod.infra.cert-manager

# Freeze reconciliation for a maintenance window
hydra argocd sync prevent prod.apps.*

# Re-enable automatic reconciliation afterward
hydra argocd sync auto prod.apps.*

# Switch a selected set to manual sync, except one known outlier
hydra argocd sync manual prod.** --exclude-app prod.infra.argocd

# Preview the sync change only
hydra argocd sync prevent prod.infra.* --exclude-app prod.infra.argocd --dry-run
```

## See Also

- [`hydra gitops status`](../cluster/status.md) - cluster-based per-app in-sync checks
- [`hydra gitops scale`](../cluster/scale.md) - scale workloads during maintenance
- [`hydra gitops uninstall`](../cluster/uninstall.md) - destructive maintenance and rebuild workflows
