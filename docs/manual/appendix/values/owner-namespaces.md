# ownerNamespaces in Values

Declaring which namespaces an app owns.

## Structure

```yaml
global:
  hydra:
    ownerNamespaces:
      - '<namespace>'
```

## Example

```yaml
global:
  hydra:
    ownerNamespaces:
      - argocd
      - argocd-system
```

## Purpose

When a namespace is listed in `ownerNamespaces`:

- Resources in that namespace are considered **owned by this app**
- During `cluster uninstall`, the namespace and all its contents can be removed
- During `cluster untracked`, resources in this namespace are not flagged as untracked

## Typical Usage

Infrastructure apps that create and manage their own namespaces:

| App | ownerNamespaces |
|-----|-----------------|
| argocd | `[argocd]` |
| cert-manager | `[cert-manager]` |
| kyverno | `[kyverno]` |
| ingress-nginx | `[ingress-nginx]` |

Application workloads typically do **not** set ownerNamespaces because their namespaces are shared or managed elsewhere.

## See Also

- [Commands: cluster uninstall](../commands/cluster/uninstall.md)
- [Commands: cluster untracked](../commands/cluster/untracked.md)
