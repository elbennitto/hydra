# Workflow: Cluster Uninstall

How to safely remove apps from a running cluster.

## Steps

### 1. Identify dependencies

```bash
hydra local refs <cluster> <appId>
```

Check if other apps depend on this one.

### 2. Create backup

```bash
hydra gitops backup create <appId>
```

### 3. Scale down (if applicable)

```bash
hydra gitops scale down <appId>
```

### 4. Uninstall

```bash
hydra gitops uninstall <appId>
```

### 5. Verify removal

```bash
hydra gitops untracked <cluster>
```

Check no orphaned resources remain.

## Safety Checklist

- [ ] No other apps reference resources from this app
- [ ] Backup created for secrets/certs
- [ ] PVCs reviewed (uninstall does not delete PVCs by default)
- [ ] DNS records cleaned up if relevant

## See Also

- [hydra gitops uninstall](../commands/cluster/uninstall.md)
- [hydra gitops backup](../commands/cluster/backup.md)
- [Refs: Ref Tags](../refs/ref-tags.md) — `[uninstall]` tag
