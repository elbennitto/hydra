# Workflow: Backup and Restore

Protect and recover cluster secrets using SOPS-encrypted backups.

## Creating Backups

```bash
# Backup one app
hydra gitops backup create prod.cluster-infra.cert-manager

# Backup all apps with backup-tagged refs
hydra gitops backup create 'prod.**'
```

Backups are stored as SOPS-encrypted YAML in the GitOps repository.

## Listing Backups

```bash
hydra gitops backup list prod.cluster-infra.cert-manager
```

## Comparing with Live State

```bash
hydra gitops backup diff prod.cluster-infra.cert-manager
```

## Restoring

```bash
hydra gitops backup restore prod.cluster-infra.cert-manager
```

## What to Back Up

Configure backup refs in values:

```yaml
global:
  hydra:
    refs:
      - ref: "/v1/Secret/cert-manager/cert-manager-ca [backup]"
```

The `[backup]` tag marks the ref for inclusion in backup operations.

## Best Practices

- Back up before any uninstall
- Back up before cluster upgrades
- Commit backups to git (they're already encrypted)
- Test restore periodically

## See Also

- [hydra gitops backup](../commands/cluster/backup.md)
- [Refs: Ref Tags](../refs/ref-tags.md) — `[backup]` tag
- [Workflow: Cluster Uninstall](cluster-uninstall.md)
