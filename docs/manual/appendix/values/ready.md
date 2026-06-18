# ready in Values

CEL-based readiness probes for resources.

## Structure

```yaml
global:
  hydra:
    ready:
      <probe-name>:
        predicate: '<CEL>'         # Which resources to check
        expressions:
          - '<CEL>'                # Readiness conditions (all must be true)
```

## Example

```yaml
global:
  hydra:
    ready:
      deployments-available:
        predicate: 'kind == "Deployment"'
        expressions:
          - 'has(entity.status.availableReplicas) && entity.status.availableReplicas > 0'
```

## Fields

### predicate

CEL expression selecting which resources this readiness check applies to.

### expressions

List of CEL expressions that must all evaluate to `true` for the resource to be considered ready. The `entity` variable provides access to the full resource object from the cluster.

## When Ready Is Evaluated

Readiness probes are evaluated during operations that wait for resource convergence, such as ordered apply sequences where downstream resources should not be applied until upstream dependencies are ready.

## See Also

- [CEL: Variables](../cel/variables.md) — The `entity` variable
- [CEL: Functions](../cel/functions.md)
