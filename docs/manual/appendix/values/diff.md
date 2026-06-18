# diff in Values

Rules for ignoring expected differences in `hydra gitops diff`.

## Structure

```yaml
global:
  hydra:
    diff:
      <rule-name>:
        predicate: '<CEL>'         # Which resources this applies to
        patches:
          - '<yq-expression>'      # Fields to remove before comparison
```

## Example: Ignore Managed Fields

```yaml
global:
  hydra:
    diff:
      managed-fields:
        predicate: 'true'
        patches:
          - 'del(.metadata.managedFields)'
```

## Example: Ignore Annotations Set by Controllers

```yaml
global:
  hydra:
    diff:
      controller-annotations:
        predicate: 'kind == "Deployment"'
        patches:
          - 'del(.metadata.annotations["deployment.kubernetes.io/revision"])'
```

## Purpose

Some fields are set by Kubernetes controllers at runtime and will always differ from the rendered template. Diff rules remove these fields from both sides before comparison, reducing noise in `hydra gitops diff` output.

## Fields

### predicate

CEL expression selecting which resources this ignore rule applies to.

### patches

YQ expressions that remove fields before diff comparison. Typically `del(...)` expressions.

## See Also

- [Commands: cluster diff](../commands/cluster/diff.md)
- [Workflow: Debugging Diffs](../workflows/debugging-diffs.md)
