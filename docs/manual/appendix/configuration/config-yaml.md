# Configuration: config.yaml

Per-context configuration file that controls Hydra's behavior.

## Location

```
gitops-repository/clusters/<context>/config.yaml
```

## Structure

```yaml
# Cluster identity
cluster: prod
environment: production

# Repository paths
chartsRepository: ../../charts-repository
gitopsRepository: .

# ArgoCD configuration
argocd:
  enabled: true
  namespace: argocd

# Allowed kubectl contexts (safety)
kubectl:
  allowedContexts:
    - prod-admin
    - prod-admin@prod
```

## Key Fields

| Field | Description |
|-------|-------------|
| `cluster` | Cluster identifier used in app IDs |
| `environment` | Environment label (production, test, etc.) |
| `chartsRepository` | Relative path to charts-repository |
| `gitopsRepository` | Relative path to gitops-repository root |
| `argocd.enabled` | Whether ArgoCD commands are available |
| `kubectl.allowedContexts` | Permitted kubectl context names |

## Multiple Contexts Example

```
gitops-repository/
  clusters/
    prod/config.yaml     # cluster: prod, env: production
    test/config.yaml     # cluster: test, env: test
    cicd/config.yaml     # cluster: cicd, env: ci
```

## See Also

- [HYDRA_CONTEXT](hydra-context.md) — Selecting the active context
- [Concepts: Context and Clusters](../concepts/context-and-clusters.md)
