# hydra gitops backup

Backup and restore Kubernetes secrets for Hydra-managed applications.

## Synopsis

```text
hydra gitops backup <subcommand> <appId> [appId...] [flags]
```

## Description

Manages secret backups as SOPS-encrypted SopsSecrets stored in the Hydra context directory. This is the safety mechanism for preserving runtime-created or controller-created secrets before destructive operations such as [`hydra gitops uninstall`](uninstall.md).

Typical examples include TLS material, database passwords, bootstrap tokens, or any Secret that exists on the cluster but is not meant to be re-generated from scratch.

The backup/restore cycle:

1. **create** — reads secrets from the live cluster and stores them as SopsSecrets in the context
2. **restore** — decrypts SopsSecrets and applies them back to the cluster
3. **list** — shows which SopsSecrets exist in the context
4. **diff** — compares backed-up secrets against the live cluster state

`create` and `restore` both support `--include` / `--exclude` as additional Secret-level filters on top of the app-defined backup selection. `list`, `restore`, and `diff` always discover backup inputs only from the selected app IDs. Backup ownership is validated from the selected app metadata, not from backup file paths.

## When To Use It

Use `hydra gitops backup` before operations that may remove or replace secrets:

- Uninstalling and reinstalling an app
- Rebuilding a cluster
- Migrating workloads that depend on generated secrets

Do not treat backup as a generic full-cluster backup. It is focused on Hydra-managed Secret data.

## Subcommands

### hydra gitops backup create

Read secrets from the live cluster and store them as SopsSecrets in the Hydra context.
Use `--include` / `--exclude` to further narrow the matched Secrets after the app-defined backup predicates have selected their candidates.
If a selected app tries to back up a Secret from a namespace that does not belong to that app, the command fails instead of writing a misplaced backup.

```text
hydra gitops backup create <appId> [appId...] [flags]
```

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--dry-run` | `-d` | Show what would be backed up without writing files |
| `--no-cluster` | | Skip cluster connection |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to include only matching Secrets (repeatable) |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude matching Secrets (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

### hydra gitops backup restore

Decrypt SopsSecrets from the Hydra context and apply them to the cluster.

Restore discovers backup inputs only from the selected app IDs and applies those Secrets directly back to the cluster. If a target Secret already exists and matches the backup after normalization, Hydra reports it as `up-to-date`. Use `--include` / `--exclude` to narrow the Secret selection explicitly. If a discovered backup targets a namespace that does not belong to the selected app, Hydra warns and reports that Secret as `skipped` instead of restoring it. Use `--create-namespaces` when the selected, ownership-valid backups target namespaces that do not exist in the cluster yet.

```text
hydra gitops backup restore <appId> [appId...] [flags]
```

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--dry-run` | `-d` | Show what would be restored without applying |
| `--no-cluster` | | Skip cluster connection |
| `--force-backup-restore` | | Force restore even when the backed-up secrets differ from what's currently in the cluster |
| `--create-namespaces` | | Create missing target namespaces for the selected backup secrets before restoring them |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to include only matching Secrets (repeatable) |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude matching Secrets (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

### hydra gitops backup list

Show which SopsSecrets exist in the Hydra context for the specified apps.

```text
hydra gitops backup list <appId> [appId...] [flags]
```

| Flag | Description |
| --- | --- |
| `--hydra-context` | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--exclude-app` | Glob pattern to exclude applications (repeatable) |
| `--no-cache` | Disable persistent Helm template cache and in-process Helm-related caches for this run |

### hydra gitops backup diff

Compare backed-up SopsSecrets against the live secrets in the cluster. Useful to verify backups are up to date before an uninstall.

```text
hydra gitops backup diff <appId> [appId...] [flags]
```

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) |
| `--color` | `-c` | Force colored output |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Backup secrets before uninstalling an app
hydra gitops backup create prod.infra.cert-manager

# Check what's backed up
hydra gitops backup list prod.infra.*

# Verify backup matches live state
hydra gitops backup diff prod.infra.cert-manager --color

# Preview a restore without changing the cluster
hydra gitops backup restore prod.infra.cert-manager --dry-run

# Restore after reinstall and create the target namespace if needed
hydra gitops backup restore prod.infra.cert-manager --create-namespaces

# Force restore when secrets have diverged
hydra gitops backup restore prod.infra.cert-manager --force-backup-restore

# Restore only a specific Secret from the available backups
hydra gitops backup restore prod.infra.cert-manager --include 'id == "v1/Secret/cert-manager/wildcard-tls"'

# Full uninstall/reinstall cycle with secret preservation
hydra gitops backup create prod.infra.cert-manager
hydra gitops uninstall prod.infra.cert-manager
hydra gitops apply prod.infra.cert-manager
hydra gitops backup restore prod.infra.cert-manager --create-namespaces
```

## Recommended Uninstall Workflow

For operations that may delete Secrets, the safe order is:

1. `hydra gitops backup diff ...` to see whether your stored backup is already current.
2. `hydra gitops backup create ...` if you need a fresh backup.
3. `hydra gitops uninstall ...`
4. `hydra gitops apply ...`
5. `hydra gitops backup restore ... --create-namespaces` when the selected backups target namespaces that do not exist yet

If restore output shows `would overwrite`, decide explicitly whether the selected backup set should really replace the live Secret:

1. Use `--force-backup-restore` when the selected backups should overwrite the cluster values.
2. Use `--include` / `--exclude` when you intentionally want to narrow or reshape the Secret selection.
3. If output shows `skipped`, move or recreate the backup under the app that actually owns the target namespace instead of forcing the restore from the wrong app.

## See Also

- [`hydra gitops uninstall`](uninstall.md) — remove resources (backup secrets first!)
- [`hydra gitops apply`](apply.md) — `--bootstrap` also creates secrets, but from the context rather than from backup
- [`hydra gitops cert-manager`](cert-manager.md) — dedicated cert-manager backup/restore
