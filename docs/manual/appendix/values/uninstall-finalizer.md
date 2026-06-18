# uninstall-finalizer in Values

Custom finalizer ordering for uninstall operations.

## Structure

```yaml
global:
  hydra:
    uninstall-finalizer:
      - '<finalizer-string>'
```

## Example

```yaml
global:
  hydra:
    uninstall-finalizer:
      - argocd.argoproj.io/finalizer
```

## Purpose

Some resources have Kubernetes finalizers that must be handled during uninstall. The `uninstall-finalizer` list tells Hydra to expect and handle these finalizers during `cluster uninstall`.

This ensures proper cleanup ordering — for example, ArgoCD Applications have finalizers that trigger cascading deletion of their managed resources.

## Common Usage

| App | Finalizer |
|-----|-----------|
| ArgoCD | `argocd.argoproj.io/finalizer` |
| Cert-Manager | `cert-manager.io/finalizer` |

## See Also

- [Commands: cluster uninstall](../commands/cluster/uninstall.md)
- [Workflow: Cluster Uninstall](../workflows/cluster-uninstall.md)
