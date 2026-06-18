# hydra gitops cert-manager

Backup and restore cert-manager resources.

## Synopsis

```text
hydra gitops cert-manager <subcommand> [flags]
```

## Description

Manages cert-manager resources (Certificates, Issuers, ClusterIssuers, etc.) separately from the general secret backup. This is useful when you need to preserve TLS certificates and their issuer configuration across reinstalls.

Note: [`hydra gitops uninstall`](uninstall.md) automatically creates a cert-manager backup unless `--skip-backup` is specified.

## When To Use It

Use this command when cert-manager objects themselves, not only Secrets, must be restored after an uninstall, cluster rebuild, or recovery procedure.

## Subcommands

### hydra gitops cert-manager restore

Restore cert-manager resources from a previously created backup into the cluster.

```text
hydra gitops cert-manager restore <cluster> [flags]
```

| Argument | Description |
| --- | --- |
| `cluster` | The cluster name (as defined in the Hydra context) |

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--dry-run` | `-d` | Show what would be restored without applying |
| `--no-cluster` | | Skip cluster connection (use with `--dry-run`) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--crd-mode` | | CRD handling: `error` or `ignore` |

## Examples

```bash
# Restore cert-manager resources after reinstall
hydra gitops cert-manager restore prod

# Preview what would be restored
hydra gitops cert-manager restore prod --dry-run

# Typical restore sequence after reinstalling cert-manager-managed apps
hydra gitops apply prod.infra.cert-manager
hydra gitops cert-manager restore prod
```

## See Also

- [`hydra gitops backup`](backup.md) — general secret backup/restore
- [`hydra gitops uninstall`](uninstall.md) — auto-creates cert-manager backup before removal
