# ArgoCD Commands

Commands for managing the ArgoCD integration.

## Contents

| Command | Description |
|---------|-------------|
| [status](status.md) | Show ArgoCD-reported sync/health status |
| [sync](sync.md) | Manage ArgoCD `AppProject` sync mode (`auto`, `manual`, `prevent`) |

## Usage

```bash
hydra argocd status 'prod.**'
hydra argocd sync prevent prod.cluster-infra.ingress-nginx
```

## Prerequisites

Requires ArgoCD to be deployed on the cluster and the `argocd` app group configured.

## ArgoCD-Managed Root Apps

Some root apps are defined under a target cluster directory but must be rendered on the management cluster because
they create ArgoCD `Application` resources there.

Mark such a root app in its own `values.yaml`.

Top-level works as before:

```yaml
global:
  hydra:
    argocd: true
```

If the root app is primarily configured through its dependency-shaped values, you can also define it there:

```yaml
<rootAppName>:
  global:
    hydra:
      argocd: true
```

When both are present, the top-level `global.hydra.argocd` value wins.

What this changes:

- The root app itself is no longer rendered as part of the target cluster.
- The root app is instead considered on `in-cluster`, where ArgoCD runs.
- Child apps of that root app still belong to the target cluster as usual.
- App IDs stay unchanged. For example, `target.platform` is still addressed as `target.platform`, but ArgoCD-facing and cluster render operations treat it as an `in-cluster` root app.

## See Also

- [hydra gitops sync](../cluster/sync.md) — Control sync modes
- [Workflow: Sync Control](../../appendix/workflows/sync-control.md)
