# Value Layering Overview

How values are merged from multiple sources into the final rendered output.

## Merge Order

Values merge bottom-up. Later sources override earlier ones:

```
1. charts-repository/apps/<group>/<app>/values.yaml         (base defaults)
        ↓ deep merge
2. gitops-repository/clusters/<cluster>/<group>/values.yaml (group-level override)
        ↓ deep merge
3. gitops-repository/clusters/<cluster>/<group>/in-cluster/<app>/values.yaml (app-level override)
        ↓ deep merge
4. Hydra ConfigMaps on cluster                              (runtime injection)
```

## Merge Semantics

- **Maps** — Deep merged. Nested keys are merged individually. A key in a later layer overrides the same key from an earlier layer.
- **Lists** — Replaced entirely. A list in a later layer replaces (not appends to) the same list from an earlier layer.
- **Scalars** — Replaced. A scalar in a later layer overrides the earlier value.

## Example: Value Flow

Given a chart with `refs.events` defined in the charts-repo:

```yaml
# charts-repository/apps/cluster-infra/cert-manager/values.yaml
global:
  hydra:
    refs:
      events:
        tag: [uninstall]
        predicate: 'gvk == "events.k8s.io/v1/Event" && ns == "cert-manager"'
```

And a cluster override adding an additional ref group:

```yaml
# gitops-repository/clusters/prod/cluster-infra/in-cluster/cert-manager/values.yaml
global:
  hydra:
    refs:
      custom-secret:
        tag: [backup]
        predicate: 'kind == "Secret" && ns == "cert-manager" && name == "ca-key"'
```

The effective result has **both** ref groups (deep merge of the `refs` map).

## Where to Define What

| Value type | Where | Why |
|-----------|-------|-----|
| Ref definitions | Charts repo | Part of the app's dependency model |
| Preset overrides | Charts repo or GitOps repo | Depends on whether it's app-global or cluster-specific |
| `kubectl.allowedContexts` | GitOps repo | Cluster-specific |
| `path`, `revision`, `repository` | GitOps repo | Cluster/environment metadata |
| `kubernetesVersion` | GitOps repo | Cluster-specific |

## Inspecting Effective Values

```bash
# Without cluster ConfigMaps
hydra local values prod.cluster-infra.cert-manager

# With cluster ConfigMaps merged
hydra gitops values prod.cluster-infra.cert-manager
```

## See Also

- [global.hydra Reference](global-hydra.md)
- [Concepts: Repositories](../concepts/repositories.md)
- [Concepts: Hydra ConfigMaps](../concepts/hydra-configmaps.md)
