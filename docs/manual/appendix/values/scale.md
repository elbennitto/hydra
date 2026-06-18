# scale in Values

Workload scaling configuration for `hydra gitops scale` commands.

## Structure

```yaml
global:
  hydra:
    scale:
      <scale-name>:
        gvk: '<group/version/kind>'      # Resource type to scale
        replicaPaths:
          - '<json-path>'                 # Path(s) to replica count
        statusReadyPath: '<json-path>'    # Path to readiness indicator
```

## Example

```yaml
global:
  hydra:
    scale:
      deployments:
        gvk: 'apps/v1/Deployment'
        replicaPaths:
          - '.spec.replicas'
        statusReadyPath: '.status.readyReplicas'
      statefulsets:
        gvk: 'apps/v1/StatefulSet'
        replicaPaths:
          - '.spec.replicas'
        statusReadyPath: '.status.readyReplicas'
```

## Fields

### gvk

The GVK (group/version/kind) of resources this scale config applies to.

### replicaPaths

JSON paths pointing to the replica count field(s) in the resource spec. Used by:
- `hydra gitops scale down` — Sets these to 0
- `hydra gitops scale up` — Restores to git-defined values

### statusReadyPath

JSON path to the field indicating how many replicas are ready. Used by `hydra gitops scale status`.

## See Also

- [Commands: cluster scale](../commands/cluster/scale.md)
- [Workflow: Scaling Workloads](../workflows/scaling-workloads.md)
