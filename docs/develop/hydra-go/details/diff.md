# Diff Architecture

## Overview

This document covers Hydra Go's diff flows. `hydra gitops diff` compares rendered Helm templates against the live Kubernetes cluster state, while `hydra gitops backup diff` compares rendered backup secrets against live cluster secrets after secret-specific normalization.

**Source files:** `cli/action/cluster_diff.go`, `cli/cmd/cluster_diff.go`, `core/commands/server_side_apply.go`, `core/commands/backup.go`

## Diff Modes

| Mode                 | Flag                 | Description                                                                                                                                                                                             |
| -------------------- | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Server** (default) | `--diff-mode=server` | Templates are sent through a server-side apply dry-run. The API server fills in defaults (clusterIP, sessionAffinity, namespace, etc.) before comparison. Produces clean diffs without false positives. |
| **Raw**              | `--diff-mode=raw`    | Templates are compared 1:1 against the cluster state. Faster, but shows differences caused by server-side defaults.                                                                                     |

Unified diff **context** (how many unchanged lines surround each hunk) is controlled by grep-style flags `--after-context` (`-A`), `--before-context` (`-B`), and `--context` (`-C`). The implementation uses `go-difflib`, which accepts a single symmetric context count; Hydra sets it to the **maximum** of any explicitly provided `-A`/`-B`/`-C` values (default **3** when none are set). See `cli/flags/diff_unified_context.go` and `entityDiff` in `cli/action/cluster_diff.go`.

## Data Flow

### Server Mode (default)

```text
hydra gitops diff app dev.demo.service-backstage
  │
  ▼
1. RenderCluster(cluster, appIds, ...)
  │  Render Helm templates → renderedEntities (KeyTemplateEntity)
  │
  ▼
2. ServerSideDryRunApplyEntities(cluster, renderedEntities, KeyTemplateEntity, KeyTemplateEntity)
  │  For each entity:
  │    a. Marshal to JSON (read from sourceKey=KeyTemplateEntity)
  │    b. Patch(ApplyPatchType, DryRun=server, FieldManager="hydra", Force=true)
  │    c. API server returns expected state with defaults filled in
  │    d. Store enriched result under targetKey=KeyTemplateEntity (overwrites in-place)
  │  Entities that fail SSA fall back to raw unstructured data
  │
  ▼
3. ListClusterAll(cluster, KeyClusterEntity)
  │  Fetch live cluster state
  │
  ▼
4. Merge(renderedEntities, clusterEntities)
  │  Match entities by GVK + namespace + name
  │
  ▼
5. Compare(KeyTemplateEntity, KeyClusterEntity)
  │  Categorize into: LeftOnly, Both, RightOnly
  │
  ▼
6. Orphan filter on RightOnly (ArgoCD tracking ID + no ownerReferences)
  │
  ▼
7. Optional CEL filter (`--include` / `--exclude`) on LeftOnly, Both, and orphan candidates
  │  Applied to merged compare entities before diff text is built (not at list time)
  │
  ▼
8. Generate unified diff
  │  LeftOnly  → additions (new resources), except rules with ignoreWhenMissingInCluster emit one line: diff ignored (resource absent in cluster): <id>
  │  Both      → modifications (entityYaml strips server metadata)
  │  RightOnly → show as deletions
  │
  ▼
Colored diff output (stdout)
```text

### Raw Mode

Same as server mode but step 2 (ServerSideDryRunApplyEntities) is skipped. Templates are compared directly against cluster state.

## Server-Side Apply Dry-Run

```go
func ServerSideDryRunApplyEntities(
    cluster *hydra.Cluster,
    entities entity.Entities,
    sourceKey types.EntityKeyUnstructured,
    targetKey types.EntityKeyUnstructured,
) (entity.Entities, error)
```

The function accepts separate `sourceKey` and `targetKey` parameters. The unstructured resource data is read from `sourceKey` and the dry-run result is stored under `targetKey`. This allows callers to preserve the original data while storing the enriched result under a different key. For example, `hydra gitops diff` uses the same key for both source and target (replacing the template data in-place), while `hydra gitops apply` uses `sourceKey=KeyTemplateEntity` and `targetKey=KeyDryRunEntity` so that the entity retains both the original template and the dry-run result for comparison.

Uses the Kubernetes dynamic client to apply each entity with:

- `types.ApplyPatchType` (server-side apply)
- `DryRun: []string{metav1.DryRunAll}` (no actual writes)
- `FieldManager: "hydra"` (ownership tracking)
- `Force: true` (resolve conflicts in favor of hydra)

The API server processes the apply and returns the expected object state including all defaults. This eliminates false-positive diffs caused by fields like:

- `spec.clusterIP`, `spec.clusterIPs` (Service)
- `spec.sessionAffinity`, `spec.type` (Service)
- `spec.internalTrafficPolicy`, `spec.ipFamilies` (Service)
- `metadata.namespace` (populated by API server)

Entities that fail SSA (e.g. unknown CRD, permission issues) fall back to raw comparison with a warning.

## KeyDryRunEntity in the Apply Flow

The `KeyDryRunEntity` key (type `EntityKeyUnstructured`) stores the server-side dry-run result of a resource. It is used during `hydra gitops apply` to detect whether an existing cluster entity has actually changed compared to the rendered template.

When applying existing entities, the flow is:

1. `ServerSideDryRunApplyEntities(cluster, existingEntities, KeyTemplateEntity, KeyDryRunEntity)` — sends each entity's template data through a server-side dry-run and stores the result under `KeyDryRunEntity`. After this step, each entity carries three unstructured keys: `KeyTemplateEntity` (original template), `KeyClusterEntity` (live cluster state), and `KeyDryRunEntity` (what the API server would produce if the template were applied).

2. `findChangedEntities()` — compares `KeyClusterEntity` against `KeyDryRunEntity` for each entity after the same **`global.hydra.diff.ignore`** normalization as `hydra gitops diff` (CEL `predicate` + `yq:` patches via `DiffIgnorePipeline`). A built-in rule removes `spec.replicas` for Deployment, ReplicaSet, and StatefulSet so pure replica drift does not trigger a re-apply. **Exception:** when the live `spec.replicas` is `0` and the dry-run desired count is `> 0`, the entity is still treated as changed (restore after scale-to-zero), using replica values read before yq normalization. DaemonSets do not use this replica path. Both sides are serialized via `PrintObject(KeepServerFieldsNo)` and compared as YAML strings. Entities with differing YAML are returned as "changed" and re-applied, along with workload entities that match the scaled-to-zero exception.

   **SSA dry-run failure fallback:** If `ServerSideDryRunApplyEntities` fails for an entity in step 1, that entity retains its original template data and `KeyDryRunEntity` is **not** set. `findChangedEntities` treats entities without `KeyDryRunEntity` conservatively as "changed" — they will be re-applied. This matches the conservative approach: when in doubt, apply.

   **Verbose debug:** When `findChangedEntities` runs with the cluster logger at DEBUG (CLI `-v`), it emits one debug line per entity with `logId` `hydra.cli.action.apply-dry-run-diff`, the resource `id`, `result` (`unchanged` / `changed` / `skipped`), and optional `reason` (`yaml_diff`, `missing_dry_run`, `restore_replicas_after_scale_zero`) so operators can grep a resource id before correlating with `hydra gitops diff`.

## Orphan Detection

Resources that exist in the cluster but not in the rendered templates are potential orphans. Orphan detection uses two filters:

1. **ArgoCD Tracking ID** — Only resources with an `argocd.argoproj.io/tracking-id` annotation matching one of the selected app IDs are considered. The annotation format is `{appId}:{group}/{kind}:{namespace}/{name}`.

2. **Owner References** — Resources with `ownerReferences` are excluded. These are controller-managed (e.g. ReplicaSets created by Deployments) and would never appear in rendered templates.

```go
func filterManagedOrphans(
    l log.Logger,
    candidates entity.Entities,
    appIds sets.Set[types.AppId],
) (entity.Entities, error)
```

## CEL resource filters (`cluster diff`)

When `--include` and/or `--exclude` are set, each entity slated for diff output (LeftOnly, Both, and post-orphan-filter RightOnly) is evaluated with the combined CEL predicate from `cli/cmd/define_flags.go` (`--exclude` is wrapped as `!(expr)`). The implementation compiles predicates via `core/cel` and filters with `EvalBool(..., MissingKeysReject)`, consistent with `hydra local find`.

Unlike `hydra gitops list` / `cluster dump`, which call `ListClusterPredicate` at fetch time, `cluster diff` must filter **after** `Merge` and `Compare` so template and cluster sides stay paired for the same resource identity.

## YAML Comparison

Both sides (template and cluster) are serialized via `yaml.PrintObject(KeepServerFieldsNo, ...)` which strips instance-specific metadata:

- `managedFields`, `creationTimestamp`, `resourceVersion`, `uid`
- `generation`, `selfLink`, `deletionTimestamp`
- `ownerReferences`, `status`

The remaining fields are compared using `go-difflib` to produce unified diffs.

## Backup Secret Diff Normalization

`hydra gitops backup diff` uses a secret-specific normalization path before generating the unified diff. The goal is to suppress false positives from representation differences and tool-managed metadata while still surfacing meaningful secret changes.

Flow:

1. `listClusterSecrets()` fetches live `v1/Secret` resources from the cluster.
2. `BackupSopsSecrets()` discovers rendered backup `SopsSecret` resources for the selected apps.
3. `decryptBackupToSecret()` converts each backup file back into a plain `v1/Secret` object map.
4. `normalizeSecretData()` is applied to both the decrypted backup secret and the live cluster secret before hashing and diffing.
5. `backupDiff()` hashes secret payloads, strips server-managed metadata, and produces a unified diff only when the normalized YAML still differs.

Normalization rules for `cluster backup diff`:

- Fold `stringData` into `data` using Kubernetes semantics, so equivalent cleartext and base64 forms compare equal.
- Remove managed annotations with the same prefixes already ignored during backup creation via `filterBackupAnnotations()`: `kubectl.kubernetes.io/`, `argocd.argoproj.io/`, and `helm.sh/`.
- If filtering removes all entries from `metadata.annotations`, remove the entire `metadata.annotations` block so an empty map does not create a diff by itself.
- Preserve custom annotations, labels, type, name, namespace, and the hashed shape of `data`, so user-managed metadata still participates in the comparison.

This keeps backup diffs aligned with backup creation semantics: annotations that Hydra already excludes from backup payloads must also be excluded when comparing backup content to cluster state. In particular, `kubectl.kubernetes.io/last-applied-configuration` must no longer produce a backup diff on its own.

### Planned Unit Tests

The implementation change should later create or update these unit tests in `hydra/hydra-go/core/commands/backup_test.go`:

- Update the normalization tests to verify that managed annotations are removed during the backup diff normalization path for all three filtered prefixes.
- Add a normalization test that verifies `metadata.annotations` is removed entirely when the last remaining annotation was filtered out.
- Add a `BackupDiff` test where backup and cluster secrets differ only by `kubectl.kubernetes.io/last-applied-configuration`; the diff must be empty and the result must be treated as up-to-date.
- Add a `BackupDiff` test where backup and cluster secrets differ only by an `argocd.argoproj.io/` annotation such as `tracking-id`; the diff must be empty and the result must be treated as up-to-date.
- Add a `BackupDiff` test where backup and cluster secrets differ only by a `helm.sh/` annotation such as `resource-policy`; the diff must be empty and the result must be treated as up-to-date.
- Add a `BackupDiff` test where a custom annotation still differs after normalization; the diff must remain non-empty so user-managed metadata changes are still reported.

## Subcommands

| Command                                     | Description                          |
| ------------------------------------------- | ------------------------------------ |
| `hydra gitops diff app <appId> [appId...]` | Diff one or more specific apps       |
| `hydra gitops diff root-app <rootAppId>`   | Diff a root app and all its children |
| `hydra gitops diff cluster <cluster>`      | Diff all apps in a cluster           |

All subcommands support `--diff-mode`, `--color`, `--no-color`, `--crd-mode`, `--network-mode`, and `--hydra-context`.
