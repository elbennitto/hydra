# Workflow: Sync Control

Manage ArgoCD reconciliation behavior.

## Normal Operation

By default, ArgoCD automatically syncs (reconciles) when it detects drift:

```bash
hydra gitops sync status 'prod.**'
# All apps should show: auto
```

## Freeze Sync for Maintenance

Prevent all sync while you investigate or perform manual fixes:

```bash
hydra gitops sync prevent 'prod.demo.*'
```

## Manual Sync Mode

Allow sync only when explicitly triggered:

```bash
hydra gitops sync manual 'prod.demo.service-auth'

# Later, trigger manually:
hydra argocd sync prod.demo.service-auth
```

## Re-enable Auto Sync

```bash
hydra gitops sync auto 'prod.demo.*'
```

## When to Use Each Mode

| Mode | Scenario |
|------|----------|
| **auto** | Normal day-to-day operation |
| **manual** | Controlled rollout, canary testing |
| **prevent** | Emergency freeze, debugging, maintenance |

## See Also

- [hydra gitops sync](../commands/cluster/sync.md)
- [hydra argocd sync](../commands/argocd/sync.md)
