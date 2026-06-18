# Commands: Backup and Restore

This file covers per-app secret backup, restore, diff, and apply-integrated backup behavior.

Back to [Commands detail index](../commands.md).

## Per-App Secret Backup System

Per-app backup and restore of Kubernetes `v1/Secret` resources as SOPS-encrypted `SopsSecret` CRDs. Each app declares which secrets to backup using CEL predicates in `global.hydra.refs` with `tag: [backup]`.

**Source file:** `core/commands/backup.go`

### Configuration

Backups are configured in the app's Helm values under `global.hydra.refs`:

```yaml
global:
  hydra:
    refs:
      backup-letsencrypt-prod-tls-secrets:
        tag:
          - backup
        desc: "TLS secrets issued by letsencrypt-prod"
        ref-parsers:
          - predicate: 'clusterEntity.annotations().getOrEmpty("cert-manager.io/issuer-name") == "letsencrypt-prod"'
      backup-letsencrypt-prod-cluster-issuer-key:
        tag:
          - backup
        desc: "Private key for the letsencrypt-prod ClusterIssuer"
        ref-parsers:
          - predicate: 'id == "v1/Secret/cert-manager/letsencrypt-prod"'
```

The `backup` tag implicitly includes the resource for uninstall. A ref group must **not** combine `backup` with `uninstall`, `uninstall-safe`, or `uninstall-force` — if this happens, `WarnDuplicateBackupUninstallTags` logs a warning.

### Backup File Location

Backups are stored as static manifests in the child app's directory:

```text
{root-app-path}/apps/{child-app-name}/backup-{namespace}-{name}.sops.yaml
```

Example: `gitops-repository/clusters/test/example-dev/in-cluster/apps/cert-manager/backup-cert-manager-letsencrypt-prod.sops.yaml`

### SopsSecret Identification

Backup SopsSecrets are distinguished from normal SopsSecrets by the annotation:

```yaml
metadata:
  annotations:
    hydra-gitops.org/hydra-backup: "true"
```

The helper function `isBackupSopsSecret(u)` checks for this annotation. It is used by:

- `collectBackupSopsSecrets` — filter rendered manifests for backup SopsSecrets
- `ConvertSopsSecretsToSecrets` (bootstrap) — skip backup SopsSecrets during bootstrap conversion
- `ExpandSopsSecretsForUninstall` — skip backup SopsSecrets during uninstall stub expansion

Backup SopsSecrets are always created with `suspend: true` so the `sops-secrets-operator` does not process them. Restore is handled exclusively by Hydra via direct Kubernetes API application.

### CLI Commands

#### `hydra gitops backup create <appIds>`

```go
func BackupCreate(cluster, appIds, networkMode, secretFilters, color, dryRun) ([]BackupResult, error)
```

Fetches secrets matching backup predicates from the cluster and stores them as SOPS-encrypted SopsSecret CRDs. `backup create` supports secret-level `--include` / `--exclude` filters in addition to the app-defined `backup` ref-parsers. For each processed secret, it reports `up-to-date` or `backed-up`. If an existing backup differs, a hash-diff is displayed.

```text
1. collectBackupGroups()
   │  Parse global.hydra.refs, collect groups with tag: [backup]
   │
   ▼
2. listClusterSecrets()
   │  Single API call: list all v1/Secret across all namespaces
   │
   ▼
3. For each backup group → for each predicate:
   │  filterSecretsByPredicate() — CEL evaluation against cached secrets
   │  → candidate secrets selected by app-defined backup refs
   │
   ▼
4. Apply CLI secret filters (`--include` / `--exclude`) to the candidate secrets
   │  Excludes are translated to `!(expr)` and composed with includes
   │
   ▼
5. For each matched secret:
   │  a. If backup file exists: decrypt → compare → show diff if changed
   │  b. convertSecretToSopsSecretYaml() — convert to SopsSecret with:
   │     - hydra-gitops.org/hydra-backup: "true" annotation
   │     - suspend: true
   │     - filtered labels/annotations (remove kubernetes.io/, helm.sh/, etc.)
   │  c. sops.EncryptSopsFile() → write to backup path
   │
   ▼
[]BackupResult
```

#### `hydra gitops backup restore <appIds>`

```go
func BackupRestore(cluster, appIds, networkMode, kubernetesVersion, secretFilters, forceRestore, color, dryRun) ([]BackupResult, error)
```

Restores secrets from backup SopsSecrets found in rendered manifests. Decrypts locally and applies `v1/Secret` directly to the cluster via `k8s.Apply()`, bypassing the sops-secrets-operator.

Restore selection is app-scoped only:

- Render only the requested `appIds`
- Discover only backup manifests belonging to those rendered apps
- Apply optional `--include` / `--exclude` filters to the decrypted target secrets
- Optionally create missing target namespaces for those already selected backup secrets via `--create-namespaces`
- Validate ownership only from rendered app metadata (`appIds`, `appNamespace`, rendered target objects), never from backup file paths
- Do not widen or narrow restore selection by namespace allowlists or `--all`

```text
1. BackupSopsSecrets()
   │  Render selected apps, filter for SopsSecret with hydra-gitops.org/hydra-backup: "true"
   │
   ▼
2. decrypt backup targets
   │  decryptBackupToSecret() — SOPS decrypt → extract secretTemplate → build v1/Secret
   │
   ▼
3. Apply CLI secret filters (`--include` / `--exclude`) to the decrypted target secrets
   │  Only filtered backups continue into restore evaluation
   │
   ▼
4. Restore candidate preparation
   │  a. Keep only backups discovered from the selected app IDs
   │  b. Apply optional secret-level filters (`--include` / `--exclude`)
   │  c. Validate ownership against rendered app metadata
   │     → app-foreign backups continue only as `skipped` restore results
   │
   ▼
5. Optional namespace preparation (`--create-namespaces`)
   │  a. List existing cluster namespaces
   │  b. Collect missing target namespaces from the filtered backup secret set
   │  c. Create only those missing namespaces before restore
   │
   ▼
6. listClusterSecrets()
   │  Single API call: list all v1/Secret across all namespaces
   │
   ▼
7. For each filtered backup secret:
   │  a. If ownership-invalid → warn + `skipped`
   │  b. Else compare with cluster secret (normalizeSecretData for stringData→data)
   │  c. If not in cluster → restoreSecretToCluster()
   │  d. If identical → `up-to-date`
   │  e. If differs → `would-overwrite` (unless --force-backup-restore)
   │
   ▼
[]BackupResult
```

Statuses: `restored`, `skipped`, `up-to-date`, `would-overwrite`, `force-restored`.

Identical restore targets are treated as an idempotent no-op and therefore report `up-to-date`, aligning restore terminology with backup create and backup diff.

The `--force-backup-restore` flag is required to overwrite a cluster secret that differs from the backup. The `--dry-run` flag previews the restore without applying changes. The `--create-namespaces` flag prepares missing target namespaces, but only for the already appId-selected, filter-selected, and ownership-valid backup secrets.

##### Backup create ownership validation

Backup creation is also ownership-validated after predicate matching:

- The selected app's backup predicates may only persist secrets that belong to that same selected app by rendered app metadata.
- If a predicate matches a secret in a namespace that does not belong to the selected app, backup creation fails fast with an error instead of writing a misplaced backup file.
- Backup output paths are a storage detail only; they must never be used to decide ownership.

#### `hydra gitops backup list <appIds>`

```go
func BackupList(cluster, appIds, networkMode, kubernetesVersion) ([]BackupResult, error)
```

Lists backup SopsSecrets found in rendered manifests. Does **not** connect to the cluster. Uses `BackupSopsSecrets()` to find SopsSecrets annotated with `hydra-gitops.org/hydra-backup: "true"` and reports their target secret IDs.

Status: `list-found`.

#### `hydra gitops backup diff <appIds>`

```go
func BackupDiff(cluster, appIds, networkMode, kubernetesVersion, color) ([]BackupResult, error)
```

Compares all secrets matched by backup predicates with the cluster state. Considers both secrets with existing backups and secrets without backups.

```text
1. listClusterSecrets()
   │  Single API call: all v1/Secret across all namespaces
   │
   ▼
2. BackupSopsSecrets()
   │  Find existing backup SopsSecrets in rendered manifests
   │
   ▼
3. collectPredicateMatchedSecretIds()
   │  Evaluate backup CEL predicates against cached cluster secrets
   │  → finds secrets that should have a backup but don't yet
   │
   ▼
4. Union of backup secret IDs + predicate-matched secret IDs
   │
   ▼
5. For each secret ID:
   │  a. Decrypt backup to plain `v1/Secret`
   │  b. Normalize backup + cluster secret before diff:
   │     - fold `stringData` into `data`
   │     - ignore managed annotations with prefixes
   │       `kubectl.kubernetes.io/`, `argocd.argoproj.io/`, `helm.sh/`
   │     - remove `metadata.annotations` entirely if it becomes empty
   │  c. No backup exists → "no-backup"
   │  d. Backup exists, secret not in cluster → "not-in-cluster"
   │  e. Backup exists, identical after normalization → "up-to-date"
   │  f. Backup exists, still differs after normalization → "changed"
   │     (with hash-diff)
   │
   ▼
[]BackupResult
```

Statuses: `list-up-to-date`, `list-changed`, `list-not-in-cluster`, `list-no-backup`.

### Apply Integration

`hydra gitops apply` uses the same app-scoped restore workflow as the explicit restore command:

1. Render the selected apps and discover restore candidates only from backup manifests belonging to those selected apps.
2. Prepare only the selected app namespaces in the apply phase plan before the restore phase runs, so restore targets can already exist when the apps are installed.
3. Call `BackupRestore` with the selected app IDs and optional secret filters only.
4. Print restore results when that restore phase runs; `Backup overview` is the authoritative restore summary, and no extra per-secret `up-to-date` lines should appear before it.
5. Abort on `would-overwrite` conflicts unless `--force-backup-restore` was provided.
6. In `--bootstrap` mode only, ordinary selected-app `SopsSecret` CRs still continue into the normal bootstrap apply path. Only backup resources that do not belong to the ownership-valid selected backup set must not continue into the later normal bootstrap apply path as ordinary resources.

This preserves the safety boundary of `cluster apply`: it restores only backup inputs rendered from the selected apps and never expands a single-child selection into all backups of the same namespace. In bootstrap mode it also prevents ownership-invalid backup resources from being applied later as normal resources. File path identity is not part of this decision. Apply logging remains phase-oriented overall, so one apply operation must not emit multiple generic `applying N resources` messages for the same resource set.

### Unit Tests (Backup Commands and Apply Integration)

The architecture requires the following tests to be added or updated:

1. `cli/cmd` flag wiring: `backup create` and `backup restore` accept `--include` / `--exclude`; `backup restore` additionally exposes `--create-namespaces`; and no backup command exposes `--all`.
2. `core/commands/backup_test.go`: backup-create filtering applies app-defined `backup` ref predicates first and CLI secret filters second.
3. `core/commands/backup_test.go`: `BackupDiff` ignores a difference that is only `kubectl.kubernetes.io/last-applied-configuration`.
4. `core/commands/backup_test.go`: `BackupDiff` ignores a difference that is only an `argocd.argoproj.io/` annotation such as `tracking-id`.
5. `core/commands/backup_test.go`: `BackupDiff` ignores a difference that is only a `helm.sh/` annotation such as `resource-policy`.
6. `core/commands/backup_test.go`: backup diff normalization removes `metadata.annotations` completely when the last remaining annotation was filtered out.
7. `core/commands/backup_test.go`: `BackupDiff` still reports a diff when a custom, user-managed annotation differs after normalization.
8. `core/commands/backup_test.go`: restore candidate discovery stays strictly appId-based, even when sibling apps share the same namespace.
9. `core/commands/backup_test.go`: restore reports `up-to-date` for an identical cluster secret instead of `already-exists`.
10. `core/commands/backup_test.go`: overwrite detection still reports `would-overwrite` and `force-restored` correctly after secret-filter evaluation.
11. `core/commands/backup_test.go`: if `BackupRestore` is reused by apply-integrated restore, the identical-secret case must keep the same `up-to-date` result there as well.
12. `core/commands/backup_test.go`: `--create-namespaces` derives missing namespaces only from the already selected restore candidate set and deduplicates shared target namespaces.
13. `core/commands/backup_test.go`: `backup create` fails when a selected app's backup predicate matches a secret that is not ownership-valid for that app.
14. `core/commands/backup_test.go`: `backup restore` warns and reports `skipped` when an ownership-invalid backup is encountered.
15. `cli/action/cluster_apply_test.go`: apply creates only namespaces for the selected apps before invoking scoped backup restore.
16. `cli/action/cluster_apply_test.go`: selecting a single child app only considers backup manifests rendered from that child app, even when other apps use the same namespace.
17. `cli/action/cluster_apply_test.go`: apply restores only ownership-valid backups discovered from the selected apps, even when other apps share the same namespace.
18. `cli/action/cluster_apply_test.go`: apply-integrated backup restore preserves the identical-secret `up-to-date` status and continues without triggering overwrite handling.
19. `cli/action/cluster_apply_test.go`: bootstrap-specific regression where an ownership-invalid backup `SopsSecret` is excluded from the later normal bootstrap apply set instead of being applied as a regular CR.
20. `cli/action/cluster_apply_test.go`: bootstrap-specific regression where ordinary selected-app `SopsSecret` resources still remain in the normal apply set.
21. `core/commands/bootstrap_test.go`: conversion-focused tests continue to verify only that backup `SopsSecret`s are not decrypted into plain `Secret`s; they must explicitly document that final bootstrap apply suppression is integration behavior covered by `cluster_apply` tests, not by conversion-only tests.

### Cluster Secret Access

All backup commands use `listClusterSecrets()` to fetch `v1/Secret` resources in a single API call (all namespaces). The result is cached as `clusterSecrets`:

```go
type clusterSecrets struct {
    byId       map[string]unstructured.Unstructured  // "v1/Secret/<ns>/<name>" → resource
    entityList []entity.Entity                       // for CEL predicate evaluation
}
```

- `get(namespace, name)` — direct lookup by namespace/name
- `entities()` — returns all secrets as `entity.Entity` for use with `filterSecretsByPredicate()`

This avoids reading the entire cluster state or making individual API calls per secret.

### Secret Data Comparison

Comparison between backup and cluster secrets involves:

1. **normalizeSecretData** — converts `stringData` entries to base64-encoded `data` entries (matching Kubernetes behavior where `stringData` wins over `data` for duplicate keys)
2. **secretHashedYaml** — replaces all `data` values with `sha256:<12-char-prefix>` hashes; removes server-managed metadata (`creationTimestamp`, `resourceVersion`, `uid`, `managedFields`)
3. **backupDiff** — produces a colored unified diff between two hashed representations

### Label and Annotation Filtering

During `convertSecretToSopsSecretYaml`, labels and annotations from the cluster secret are filtered before storage:

- **filterBackupLabels** — removes labels containing `kubernetes.io/` or `helm.sh/`
- **filterBackupAnnotations** — removes annotations starting with `kubectl.kubernetes.io/`, `argocd.argoproj.io/`, or `helm.sh/`

### Bootstrap and Uninstall Safety

Backup SopsSecrets (`hydra-gitops.org/hydra-backup: "true"`) are explicitly **skipped** by:

- **ConvertSopsSecretsToSecrets** (bootstrap) — does not convert backup SopsSecrets into plain `v1/Secret`s, preventing conflicts with the suspend mechanism
- **ExpandSopsSecretsForUninstall** — does not create derived `v1/Secret` stubs from backup SopsSecrets

This ensures backup secrets are only managed through `hydra gitops backup restore`, not through the bootstrap or sops-secrets-operator flows.
For `hydra gitops apply --bootstrap`, this rule is tightened further: normal selected-app `SopsSecret` CRs still proceed through bootstrap conversion and apply, but any backup resource outside the selected backup manifest set is also excluded from the later normal bootstrap apply path. This stricter exclusion is bootstrap-specific for now.

### Backup Implies Uninstall

`MarkAsSelectedByUninstallPredicates` collects both `uninstall` and `backup` predicates via `HydraAppBackupPredicates()` and includes them in the uninstall selection. This means any secret matched by a `backup` predicate is automatically included during `hydra gitops uninstall`.

### Race Condition Prevention

The backup system prevents the cert-manager race condition (cert-manager creates cert → backup converts to SopsSecret → sops-secrets-operator overwrites the cert) by:

1. SopsSecret CRDs are stored with `suspend: true` — the sops-secrets-operator ignores them
2. Restore decrypts locally and applies `v1/Secret` directly via `k8s.Apply()` — no SopsSecret is ever set to `suspend: false`
3. Bootstrap skips backup SopsSecrets — they are never converted to plain secrets during cluster setup
