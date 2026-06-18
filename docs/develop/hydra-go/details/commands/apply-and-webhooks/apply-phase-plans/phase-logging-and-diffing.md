# Apply and Webhooks: Phase Logging and Diffing

This page covers the shared phase log format, related test coverage, and the diff helper that decides which resources must be re-applied.

Back to [Apply Phase Plans](../apply-phase-plans.md).

## Phase Logging

All phase log messages in `ClusterApply` and `DeleteResources` use a consistent format that shows both the current phase number and the total phase count:

```text
"phase {current}/{total}: {description}"
```

**Examples** (`total` matches the built plan for that run, often 5–9 for apply):

```text
phase 1/5: applying CRDs
phase 3/5: applying {count} main resources at template scale
phase 5/5: deleting orphaned resources
```

When **`--disable-webhooks`** is set, the same phase appends the suffix “(excluding webhooks)” to the apply message, so it is obvious that webhook objects are handled in later phases together with the webhook-disable choreography.

### Skipped Phases

When a phase has nothing to do (e.g., no CRDs to apply, no orphans to delete), it logs a `"(skipped)"` suffix instead of being silently omitted:

```text
"phase {current}/{total}: {description} (skipped)"
```

**Examples:**

```text
phase 1/5: applying CRDs (skipped)
phase 5/5: deleting orphaned resources (skipped)
```

Optional **behaviors** controlled by flags (integrated backup restore, non-ready webhook disable, scale-up, orphan scale-down) are **not** registered as phases when those flags are unset, so they produce **no** log line—not even `(skipped)`. The final **orphan delete** phase is **always** present and deletes **all** identified orphans (including webhook configs). Phases that **are** in the plan but have no work (for example no CRDs in the selection) still log with `(skipped)` so numbering stays contiguous from 1 through `total`.

### Total Phase Counts

`ClusterApply` builds the phase list from `buildApplyPhases`: **always** CRDs, namespaces, main workload apply, applying webhooks, then **deleting orphaned resources** (entire orphan set: webhooks and everything else Hydra classifies as orphaned for the selection). **Conditionally** included: backup restore (`--backup-restore` and not `--skip-backup-restore`), disabling non-ready webhooks (`--disable-webhooks`, implied by `--bootstrap`), scale-up (`--scale-up`), and **only** scaling down orphaned workloads (`--orphan-scale-down`) before the final delete. A typical apply without optional flags has **5** phases; with all optional steps enabled, the maximum is **9**.

| Mode        | Total phases | Notes                                                                 |
| ----------- | ------------ | --------------------------------------------------------------------- |
| Apply       | 5–9          | See `buildApplyPhases` in `cli/action/cluster_apply_plan.go`          |
| Uninstall   | 3            | Three internal phases (webhook delete, scale-down, delete resources). |

### Integration with DeleteResources

`DeleteResources` (used by `hydra gitops uninstall`, not by `ClusterApply`) uses `phaseOffset` for its webhook deletion phase, `phaseOffset+1` for its scale-down phase, and `phaseOffset+2` for its deletion phase. Callers pass the overall `totalPhases` for consistent `phase k/total` logging.

| Mode      | `phaseOffset` | `totalPhases` | Example               |
| --------- | ------------- | ------------- | --------------------- |
| Uninstall | 1             | 3             | phase 1/3 through 3/3 |

`ClusterApply` orphan cleanup is implemented as phases in `cluster_apply_plan.go`, not via `DeleteResources`.

### Dry-Run Prefix in DeleteResources

When dry-run mode is active, all individual action log messages inside `DeleteResources` must include a `[dry-run]` prefix:

```text
[dry-run] scaling down apps/v1/Deployment/demo/my-deployment from 3 to 0 replicas
[dry-run] deleting apps/v1/Deployment/demo/my-deployment
[dry-run] removing finalizers from v1/Pod/demo/my-pod
```

Previously only a generic "running in Dry Run Mode" message was shown at the start of `DeleteResources`. The individual "scaling down {entity}" and "deleting {entity}" messages did not indicate dry-run status, making it unclear to the user whether actions were actually being performed.

### Unit Tests (Phase Logging)

Test file: `cli/action/cluster_apply_test.go` (for `ClusterApply` integration) and `core/commands/delete_test.go` (for `DeleteResources`)

#### ClusterApply Phase Logging

1. **Variable total** — `buildApplyPhases` includes optional phases only when their flags are set; the final orphan-delete phase is always included. `total` in logs matches the built list length.
2. **Automatic numbering** — phase log messages match `"phase {n}/{total}: ..."` and the numbers come from builder order.
3. **Omitted optional steps** — when backup restore, webhook disable, scale-up, or orphan scale-down is not part of the plan, there is no log line for it (not a `(skipped)` line). The final orphan-delete phase is always in the plan.
4. **Skipped CRD phase** — when no CRDs exist, the CRD phase still runs and logs `(skipped)` with the correct `current/total`.
5. **Skipped namespace phase** — same pattern when no namespaces exist.
6. **Skipped orphan scale-down** — when `--orphan-scale-down` is set but there is no work, that phase logs `(skipped)`.
7. **Skipped orphan delete** — when there are no orphans, the final phase logs `(skipped)`.
8. **Skipped webhook disable phase** — when webhook disable is in the plan but every provider is ready (or nothing to disable), that phase logs `(skipped)` as appropriate.

#### DeleteResources Phase Logging

`DeleteResources` is used by `hydra gitops uninstall` (not by `ClusterApply`; apply orphan cleanup uses `cluster_apply_plan` phases). Messages:

1. **Phase offset uninstall** — `DeleteResources(phaseOffset=1, totalPhases=3)` logs `"phase 1/3: deleting webhook configurations"`, `"phase 2/3: scaling down workloads before deletion"`, and `"phase 3/3: deleting {N} resources"`
2. **Other callers** — may pass different `phaseOffset` / `totalPhases` so deletion appears as a contiguous tail of a larger plan
3. **No webhooks to delete** — `DeleteResources` with entities that have no webhook configs logs `"phase {phaseOffset}/{total}: deleting webhook configurations (skipped)"`
4. **No workloads to scale** — `DeleteResources` with entities that have no workloads logs `"phase {phaseOffset+1}/{total}: scaling down workloads before deletion (skipped)"`
5. **Dry-run prefix on scale-down** — individual "scaling down {entity}" messages include `[dry-run]` prefix when dry-run mode is active
6. **Dry-run prefix on delete** — individual "deleting {entity}" messages include `[dry-run]` prefix when dry-run mode is active
7. **Dry-run prefix on finalizer removal** — "removing finalizers from {entity}" messages include `[dry-run]` prefix when dry-run mode is active
8. **No dry-run prefix when not dry-run** — action messages do NOT include `[dry-run]` prefix when dry-run mode is inactive
9. **`TestDeleteResources_WebhookPhaseLogging`** — verify that webhook deletion phase log message appears with correct phase numbering for the passed `phaseOffset` and `totalPhases`

#### collectScaleDownTargets — Owner Filtering Tests

Test file: `core/commands/delete_test.go`

1. **`TestCollectScaleDownTargets_ReplicaSetOwnedByDeployment`** — ReplicaSet with `ownerReference` to a Deployment is excluded from scale targets (tests the unexported `collectScaleDownTargets` used by `DeleteResources`)
2. **`TestCollectScaleDownTargets_BareReplicaSet`** — ReplicaSet without `ownerReferences` is included as a scale target (tests the unexported `collectScaleDownTargets` used by `DeleteResources`)

#### collectScaleDownTargets — Custom Workload Tests

Test file: `core/commands/delete_test.go`

| Test                                            | Verifies                                                                           |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- |
| `TestCollectScaleDownTargets_CustomWorkload`    | Kafka CR with custom replicaPaths → returns ScaleTarget with IsCustomWorkload=true |
| `TestCollectScaleDownTargets_CustomAndBuiltIn`  | Mix of Deployment + Kafka CR → both returned                                       |
| `TestCollectScaleDownTargets_NoCustomWorkloads` | No customWorkloads parameter → backward compatible                                 |

#### CollectScaleTargets — Owner Filtering Tests

Test file: `core/commands/scale_test.go`

1. **`TestCollectScaleTargets_ReplicaSetOwnedByDeployment`** — ReplicaSet with `ownerReference` to a Deployment is excluded from scale targets (tests the exported `CollectScaleTargets` used by `ScaleDownWorkloads`/`ScaleUpWorkloads`)
2. **`TestCollectScaleTargets_BareReplicaSet`** — bare ReplicaSet is included via the exported `CollectScaleTargets`

## Helper Function: `findChangedEntities`

**Source file:** `cli/action/cluster_apply.go`

```go
func findChangedEntities(entities entity.Entities) (entity.Entities, error)
```

Accepts entities that carry all three unstructured keys (`KeyTemplateEntity`, `KeyClusterEntity`, `KeyDryRunEntity`) and returns only those where the cluster state differs from the dry-run result.

For workload kinds (`Deployment`, `ReplicaSet`, `StatefulSet`), `spec.replicas` is zeroed in deep copies of both the cluster and dry-run unstructured objects before comparison so that pure replica scaling does not trigger a re-apply (for example differences between live replica count and template count when an HPA or manual scale is in play). **Exception:** if the live object has an explicit `spec.replicas` of `0` and the dry-run desired count is greater than `0`, the entity is still treated as changed so a workload that was scaled to zero can be brought back to template scale. `DaemonSet` entities are **not** zeroed because they do not have `spec.replicas`.

Both sides are serialized via `PrintObject(KeepServerFieldsNo)` and compared as YAML strings. Only entities whose YAML differs are included in the returned set, plus workload entities that match the scaled-to-zero exception above.

Entities that are missing `KeyDryRunEntity` (e.g. because the server-side dry-run failed for that entity) are conservatively treated as changed and included in the result. This ensures that when the dry-run cannot confirm whether an entity has changed, it is re-applied rather than skipped.

### Unit Tests (findChangedEntities)

Test file: `cli/action/cluster_apply_test.go`

1. **Unchanged entity** — entity where cluster YAML == dryrun YAML → not in result
2. **Changed entity** — entity where cluster YAML != dryrun YAML → in result
3. **Workload with only replica difference (non-zero)** — Deployment where only `spec.replicas` differs between cluster and dryrun and **both** sides are non-zero → not in result (replicas zeroed before diff)
4. **Workload scaled to zero vs template** — Deployment where cluster `spec.replicas` is `0`, dry-run `spec.replicas` is `> 0`, and there is no other spec difference → in result (restore)
5. **Workload with replica and other differences** — Deployment where `spec.replicas` AND other fields differ → in result
6. **StatefulSet with only replica difference** — StatefulSet where only `spec.replicas` differs → not in result
7. **ReplicaSet with only replica difference** — ReplicaSet where only `spec.replicas` differs → not in result
8. **DaemonSet unchanged** — DaemonSet with identical cluster and dryrun YAML → not in result (no replica zeroing needed)
9. **DaemonSet with changes** — DaemonSet where cluster YAML != dryrun YAML → in result (confirms DaemonSet changes are detected and no false replica-zeroing is attempted)
10. **Entity without KeyDryRunEntity (SSA failure)** — entity that has KeyClusterEntity but no KeyDryRunEntity → treated as changed (conservative fallback)
11. **Non-workload changed entity (ConfigMap)** — ConfigMap where cluster YAML != dryrun YAML → in result (confirms no replica-zeroing on non-workloads)
12. **Empty Both set** — when no entities are in both template and cluster → no diffing log message, no dry-run call

### Unit Tests (ServerSideDryRunApplyEntities — separate source/target keys)

Test file: `core/commands/server_side_apply_test.go`

1. **Different source and target keys** — source data is read from `sourceKey` and result is written to `targetKey`; original `sourceKey` data is preserved unchanged
2. **Same source and target key** — behaves like the original single-key signature; result overwrites the source data in-place
