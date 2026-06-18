# Workflow: Cluster System

Audit and manage the cluster inventory using presets.

## Goal

Understand which resources on the cluster are known/managed and which are untracked.

## Steps

### 1. Show preset matches

```bash
hydra gitops system <cluster>
```

Review which presets are active and what they match.

### 2. Find untracked resources

```bash
hydra gitops untracked <cluster>
```

Resources that appear here are neither:
- Rendered by an app's templates
- Claimed by a preset

### 3. Resolve untracked resources

For each untracked resource, decide:
- **Add to a preset** — If it's infrastructure installed outside Hydra
- **Add refs in an app** — If an app should own it
- **Remove it** — If it's genuinely orphaned
- **Ignore it** — If it's expected (e.g., kube-system defaults)

### 4. Validate

```bash
hydra gitops system <cluster>
hydra gitops untracked <cluster> --exclude 'namespace == "kube-system"'
```

## See Also

- [hydra gitops system](../commands/cluster/system.md)
- [hydra gitops untracked](../commands/cluster/untracked.md)
- [Presets](../presets/)
