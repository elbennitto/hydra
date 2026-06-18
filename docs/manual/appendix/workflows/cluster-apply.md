# Workflow: Cluster Apply

Day-to-day workflow for deploying changes to a running cluster.

## Steps

### 1. Make changes locally

Edit values, templates, or chart versions in the charts-repository.

### 2. Validate locally

```bash
hydra local template '<cluster>.<app>'
hydra local test refs '<cluster>.**'
hydra local review '<cluster>.<app>'
```

### 3. Check the diff

```bash
hydra gitops diff '<cluster>.<app>'
```

Review the output. Expect only your intended changes.

### 4. Apply

```bash
hydra gitops apply '<cluster>.<app>'
```

### 5. Verify

```bash
hydra gitops status '<cluster>.<app>'
hydra gitops diff '<cluster>.<app>'  # Should be empty now
```

## Handling Immutable Fields

Some Kubernetes fields cannot be patched (e.g., Service `clusterIP`, Job `selector`). If the diff shows changes to immutable fields:

```bash
hydra gitops apply '<cluster>.<app>' --replace
```

> **Warning**: `--replace` deletes and recreates the affected resource, causing downtime.

## See Also

- [Workflow: Debugging Diffs](debugging-diffs.md)
- [hydra gitops apply](../commands/cluster/apply.md)
- [hydra gitops diff](../commands/cluster/diff.md)
