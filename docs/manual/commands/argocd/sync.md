# hydra argocd sync

Trigger ArgoCD sync for apps.

## Synopsis

```bash
hydra argocd sync <appId...>
```

## Description

Triggers an ArgoCD reconciliation (sync) for the specified apps. This forces ArgoCD to pull the latest desired state from Git and apply it.

## Examples

```bash
hydra argocd sync prod.cluster-infra.ingress-nginx
hydra argocd sync 'prod.demo.*'
```

## See Also

- [hydra gitops sync](../cluster/sync.md) — Control sync modes (auto/manual/prevent)
- [hydra argocd status](status.md)
