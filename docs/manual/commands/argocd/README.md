# ArgoCD Commands

Commands for managing the ArgoCD integration.

## Contents

| Command | Description |
|---------|-------------|
| [status](status.md) | Show ArgoCD-reported sync/health status |
| [sync](sync.md) | Trigger ArgoCD sync for apps |

## Usage

```bash
hydra argocd status 'prod.**'
hydra argocd sync prod.cluster-infra.ingress-nginx
```

## Prerequisites

Requires ArgoCD to be deployed on the cluster and the `argocd` app group configured.

## See Also

- [hydra gitops sync](../cluster/sync.md) — Control sync modes
- [Workflow: Sync Control](../../appendix/workflows/sync-control.md)
