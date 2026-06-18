# hydra gitops sync

Manage ArgoCD `AppProject` sync for Hydra-managed applications.

## Synopsis

```text
hydra gitops sync <status|auto|manual|prevent> ...
```

## Description

`hydra gitops sync` controls whether ArgoCD may automatically reconcile selected applications, allows only manual sync, or blocks sync entirely. Sync is configured on ArgoCD `AppProject` resources in the `argocd` namespace.

For the same mutating operations, [`hydra argocd sync`](../argocd/README.md) is the primary documented surface. `hydra gitops sync` remains available for workflows that prefer the `hydra gitops` command family.

### hydra gitops sync status

Read-only: list configured AppProject sync and related ArgoCD status for the in-cluster ArgoCD instance. Optional [App IDs](../README.md#app-ids) (including [wildcards](../README.md#wildcards)) filter the view.

For each root application group, output is a small table: column headers **`State`**, **`Mode`**, and **`Operation`**, then one row per Application with aligned values. The **`Operation`** column shows the operation phase and duration (without the internal `sync operation:` prefix), or the fallback text **`none`** in gray when there is no operation state. **`Application.status.conditions`** (same as **App conditions** in the ArgoCD UI) are printed below the affected row, indented. The **condition line block width** matches the **global maximum table row width across the current command output** (through the end of **Operation**) minus the extra indent on condition lines. When a line is longer than that width, the **first** colon-space pair **at or after** that width is turned into a newline after the colon (same effect as replacing the space after that colon with a line break); the text **before** that pair is left unchanged, and only the **remainder to the right** is processed the same way again. **Then**, any segment that is still too long and has no further colon-space break is word-wrapped to that width.

```text
hydra gitops sync status [appId...] [flags]
```

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--color` | `-c` | Force colored output |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--qps` | | Kubernetes REST client QPS override inherited from `hydra gitops` (`0` = client-go default, negative disables client-side throttling) |
| `--api-burst` | | Kubernetes REST client burst override inherited from `hydra gitops` (requires positive `--qps`) |

### hydra gitops sync auto | manual | prevent

Mutating: set sync behavior for one or more applications. Requires at least one app ID.

```text
hydra gitops sync <auto|manual|prevent> <appId> [appId...] [flags]
```

| Mode | Effect |
| ---- | ------ |
| `auto` | Allow automatic sync (green in ArgoCD UI) |
| `manual` | Disable automatic sync; manual sync still allowed (yellow) |
| `prevent` | Block automatic and manual sync (red) |

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--color` | `-c` | Force colored output |
| `--dry-run` | `-d` | Simulate without mutating cluster resources |
| `--no-cluster` | | Skip cluster connection and Kubernetes context validation; resolve the selected apps only |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--qps` | | Kubernetes REST client QPS override inherited from `hydra gitops` (`0` = client-go default, negative disables client-side throttling) |
| `--api-burst` | | Kubernetes REST client burst override inherited from `hydra gitops` (requires positive `--qps`) |

`--dry-run` and `--no-cluster` are mutually exclusive.

## See also

- [`hydra argocd status`](../argocd/README.md) — ArgoCD-reported application sync state
- [`hydra argocd sync`](../argocd/README.md) — canonical sync control
- [`hydra gitops status`](status.md) — rendered desired state vs live cluster (different from AppProject sync)
