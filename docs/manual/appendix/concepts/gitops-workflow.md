# GitOps Workflow

How changes flow from Git to the cluster.

## The Flow

```
Developer commits to Git
        │
        ▼
┌─────────────────────┐
│  Hydra CI Pipeline  │  (test → release → promote → publish)
└─────────────────────┘
        │
        ▼
┌─────────────────────┐
│  ArgoCD watches Git │  (continuous reconciliation)
└─────────────────────┘
        │
        ▼
┌─────────────────────┐
│  Cluster applies    │  (server-side apply)
└─────────────────────┘
        │
        ▼
┌─────────────────────┐
│  Hydra verifies     │  (diff, status, review)
└─────────────────────┘
```

## Steady-State: ArgoCD Auto-Sync

In normal operation:

1. A developer changes a chart in the charts-repository
2. CI runs `hydra ci run test` → `hydra ci run release` → `hydra ci run publish`
3. The GitOps repository gets updated (version bump or promote MR)
4. ArgoCD detects the change and syncs automatically
5. The cluster converges to the new desired state

Hydra is not in the runtime path — ArgoCD handles continuous delivery.

## Manual Operations: Hydra CLI

Hydra CLI is used for operations that ArgoCD cannot handle:

| Operation | Command |
|-----------|---------|
| Initial cluster bootstrap | `hydra gitops apply --bootstrap` |
| Inspect desired vs. live state | `hydra gitops diff` |
| Emergency manual deploy | `hydra gitops apply` |
| Freeze ArgoCD sync | `hydra gitops sync prevent` |
| Scale down for maintenance | `hydra gitops scale down` |
| Audit unmanaged resources | `hydra gitops untracked` |

## Git as Single Source of Truth

The desired cluster state is fully defined in Git:

- **Charts repo** — Application definitions and default values
- **GitOps repo** — Per-cluster configuration and overrides

The live cluster state should always converge toward what Git defines. `hydra gitops diff` shows the delta.

## Feedback Loop

After any change, verify convergence:

```bash
# Are there any diffs?
hydra gitops diff 'prod.**'

# Are all apps in sync?
hydra gitops status 'prod.**'

# Are there resources outside Hydra's management?
hydra gitops untracked prod
```

## See Also

- [Workflows: Cluster Apply](../workflows/cluster-apply.md)
- [Workflows: Sync Control](../workflows/sync-control.md)
- [Workflows: CI/CD Integration](../workflows/ci-cd-integration.md)
