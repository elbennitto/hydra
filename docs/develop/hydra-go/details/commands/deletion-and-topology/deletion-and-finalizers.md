# Deletion and Topology: Deletion and Finalizers

This page covers uninstall finalizer cleanup, resource deletion behavior, orphan handling, and the full uninstall execution flow.

Back to [Deletion and Topology](../deletion-and-topology.md).

## Finalizer Removal Commands

### RemoveUninstallFinalizers

```go
func RemoveUninstallFinalizers(h hydra.Hydra, clusterEntities entity.Entities, appIds sets.Set[types.AppId]) error
```text

Removes finalizers specified in `global.hydra.uninstall-finalizer` from all cluster resources that carry them. This directly patches resources via the Kubernetes API using `MergePatchType`.

```text
1. HydraAppUninstallFinalizers(cluster, appIds, HelmNetworkModeOffline) → finalizerNames
   If empty → return (nothing to do)
   │
   ▼
2. collectFinalizerPatches(clusterEntities, KeyClusterEntity, finalizerNames)
   For EACH entity in clusterEntities (cluster-wide, all namespaces + cluster-scoped):
   │  Check if entity has any of the listed finalizers
   │  Collect patches: keep only non-matching finalizers
   │  If no patches → return (nothing to do)
   │
   ▼
3. For each patch:
   │  Log per removed finalizer: INFO "removing finalizer {finalizer} from {entity}"
   │  (logged once per finalizer, so multiple log lines if multiple finalizers match)
   │  Patch the resource via Kubernetes dynamic client (MergePatchType)
   │  (other finalizers on the same resource are preserved)
   │  NotFound errors are handled gracefully (debug log, no abort)
   │
   ▼
4. Dry-run: changes are logged as "[dry-run] removing finalizer {finalizer} from {entity}" but not executed
```

**Pure function for testability:**

```go
func collectFinalizerPatches(entities entity.Entities, key types.EntityKeyUnstructured, finalizers []string) ([]FinalizerPatch, error)
```text

Where `FinalizerPatch` is:

```go
type FinalizerPatch struct {
    Id                types.Id
    FinalizersToKeep  []string
    FinalizersRemoved []string
}
```

This pure function computes the list of patches without side effects. `RemoveUninstallFinalizers` calls it and then applies the patches via the Kubernetes API.

See [uninstall-finalizer](uninstall-selection.md#uninstall-finalizer) for the configuration-level behavior.

## Deletion Commands

### DeleteResources

```go
func DeleteResources(
    h hydra.Hydra,
    deletes entity.Entities,
    key htypes.EntityKeyUnstructured,
    forceScaleDown htypes.ForceScaleDown,
    scaleTimeout time.Duration,
    phaseOffset int,
    totalPhases int,
) error
```text

Deletes selected entities from the cluster using a three-phase strategy. The `phaseOffset` and `totalPhases` parameters integrate the internal phases into the caller's overall phase numbering. Both values are supplied by the caller's phase plan, so `DeleteResources` does not assume fixed numbers such as `7/9` or `8/10`. See [Phase Logging](../apply-and-webhooks/apply-phase-plans/phase-logging-and-diffing.md#phase-logging) for the log format.

```text
Phase {phaseOffset}/{totalPhases}: Delete webhook configurations
  │  1. SplitWebhooks(deletes) → webhookEntities, rest
  │     Identifies ValidatingWebhookConfiguration and MutatingWebhookConfiguration
  │     entities from the deletion set.
  │  2. If webhookEntities is non-empty:
  │     k8s.DeleteWebhookConfigs(ctx, l, dynamicClient, webhookEntities, key, dryRun)
  │     Removes webhook configs from the cluster BEFORE scale-down to prevent
  │     admission webhooks from blocking subsequent API operations when the
  │     webhook's backing service is being deleted.
  │  3. If no webhook entities: log "phase {phaseOffset}/{totalPhases}: deleting webhook configurations (skipped)"
  │
  ▼
Phase {phaseOffset+1}/{totalPhases}: Scale-Down + Wait (using TopologicalExecute with reversed edges)
  │  TopologicalExecute(l, workloadEntities, ReverseRefs(refs),
  │    start = scale down workload,
  │    waitReady = poll until scaled down)
  │
  │  1. collectScaleDownTargets() identifies workloads needing scale-down
  │  2. Deployment/StatefulSet/ReplicaSet: patch spec.replicas = 0
  │  3. DaemonSet: patch spec.template.spec.nodeSelector = {"hydra-gitops.org/hydra-disabled": "true"}
  │  4. Poll every 2s (configurable timeout, default 10m per workload):
  │     - Deployment/StatefulSet/ReplicaSet: check .status.replicas == 0
  │     - DaemonSet: check .status.currentNumberScheduled == 0
  │  5. Dependents scale down before dependencies (reversed edges).
  │     Workloads at the same level scale concurrently.
  │  6. On timeout:
  │     - Without --force-scale-down: abort (ErrScaleDownTimeout)
  │     - With --force-scale-down: force-delete remaining pods (grace-period=0)
  │
  ▼
Phase {phaseOffset+2}/{totalPhases}: Foreground Deletion (using TopologicalExecute with reversed edges)
  │  TopologicalExecute(l, allEntities, ReverseRefs(refs),
  │    start = remove finalizers + delete,
  │    waitReady = confirm deleted)
  │
  │  Up to 3 passes. Each entity:
  │    1. Remove finalizers (patch)
  │    2. Delete with DeletePropagationForeground
  │
  ▼
Complete
```

**Dry-run mode:** When `config.DryRun` is true, both scale-down and deletion are logged but not executed. In dry-run mode, the `[dry-run]` prefix is prepended to each individual action log message (e.g. `[dry-run] scaling down web-app to 0 replicas`, `[dry-run] deleting apps/v1/Deployment/demo/my-deployment`). There must be NO separate dry-run log line — the prefix on the action message itself is sufficient. Without dry-run, the same messages appear without the prefix. The generic "running in Dry Run Mode" message alone is not sufficient.

**Workload types:** Deployment, ReplicaSet, StatefulSet, and DaemonSet. Deployment/ReplicaSet/StatefulSet are scaled down by setting `spec.replicas = 0`. DaemonSets are scaled down by patching an impossible `nodeSelector` (`{"hydra-gitops.org/hydra-disabled": "true"}`), since DaemonSets don't have `spec.replicas`. Scale-up restores the original `nodeSelector` from the rendered template. ReplicaSets are included for completeness; in practice, only bare ReplicaSets (not managed by a Deployment) should appear in rendered templates.

**Timeout behavior:** When the polling timeout (configured via `--scale-timeout`, default `10m`) expires without all pods terminating:

- Without `--force-scale-down`: the operation aborts with `ErrScaleDownTimeout` and the message: _"aborted: pods did not terminate within {timeout}. To retry uninstall, run the same command again. To re-scale workloads, run: hydra gitops scale \<params\> up"_
- With `--force-scale-down`: remaining pods are force-deleted with `GracePeriodSeconds: 0`, then Phase 2 proceeds.

**New error:** `ErrScaleDownTimeout` defined in `base/errors/errors.go`.

**New type:** `ForceScaleDown bool` defined in `core/types/hydra.go`.

Scale target collection, custom workload support, and the related test coverage are documented in [Scale Targets](topology-and-scaling/scale-targets.md#scale-target-collection).

## Entity Utility Functions

### OrphanedEntities (Entity Package)

**Source file:** `core/entity/entities_group.go`

```go
func (entities Entities) OrphanedEntities(key types.EntityKeyUnstructured) Entities
```text

Detects entities whose `ownerReferences` ALL point to UIDs that are not present in the entity collection. These are "orphaned" resources — their parent was deleted but the child remains stuck in the cluster (e.g., Pods lingering after their StatefulSet was deleted, breaking the ownership chain).

**Algorithm:**

```text
1. Build UidMap of all entities in the collection
   │
   ▼
2. For each entity:
   │  a. If entity has no unstructured data → skip
   │  b. Get ownerReferences from unstructured data
   │  c. If no ownerReferences → skip (root resource, not orphaned)
   │  d. Check if ALL ownerReference UIDs are missing from the UidMap
   │     ALL missing → entity is orphaned
   │     At least one exists → entity is NOT orphaned
   │
   ▼
3. Return new Entities collection containing only the orphans
```

**Key design decision:** An entity is only considered orphaned if ALL of its ownerReferences point to non-existent UIDs. If even one owner still exists in the collection, the entity is not orphaned. This avoids false positives for resources with multiple owners where only some were deleted.

**Namespace-agnostic:** The function operates on whatever entity collection it is called on. Namespace filtering is the caller's responsibility.

**Reusable:** Can be used by uninstall, apply, and future operations that need to detect broken ownership chains.

#### Usage in handleLeftovers

After the existing ownership-based matching (which finds leftovers owned by entities selected for uninstallation), `handleLeftovers` calls `leftovers.OrphanedEntities(types.KeyClusterEntity)` to detect orphaned resources. The leftover collection at this point is the set of cluster entities in the selected namespaces minus the uninstall set, so `OrphanedEntities` detects entities whose owners were already deleted from the cluster. The resulting orphans are merged into the uninstall set.

#### Unit Tests (OrphanedEntities)

Test scenarios for the `OrphanedEntities` function:

1. No entities with ownerReferences — returns empty
2. Entity with ownerRef pointing to existing UID — not orphaned
3. Entity with ownerRef pointing to non-existent UID — orphaned
4. Entity with multiple ownerRefs, all non-existent — orphaned
5. Entity with mixed ownerRefs (one exists, one doesn't) — NOT orphaned
6. Entity without ownerReferences — not orphaned (root resource)
7. Entity without unstructured data — skipped
8. Multi-level orphan chain — both levels detected

## Data Flow (Uninstall)

```text
hydra gitops uninstall production.monitoring.prometheus --hydra-context ./gitops [--force | --keep | --force-all]
  │
  ▼
action.ClusterUninstall()
  │
  ├── 1. RenderCluster(cluster, config, appIds)
  │       Render templates for the apps to uninstall
  │       → templateEntities
  │
  ├── 2. ListClusterAll(cluster, config)
  │       Fetch live cluster state
  │       → clusterEntities
  │
  ├── 3. selectUninstallStuff()
  │       MarkAsSelectedArgoCdManagedResources(clusterEntities, appIds)
  │       MarkAsSelectedByUninstallPredicates(clusterEntities, hydraValues)
  │         (includes both uninstall and backup predicates — backup implies uninstall)
  │       ExclusiveNamespaces(templateEntities, clusterEntities, appIds)
  │       → mark resources for uninstall, find safe-to-delete namespaces
  │
  ├── 4. handleLeftovers()
  │       Add owned-by resources to the uninstall set
  │       Detect orphaned resources (ownerRefs pointing to deleted UIDs)
  │       → updated uninstall entities
  │
  ├── 5. handleForceLeftovers(cluster, clusterEntities, uninstalls, namespaces, appIds, forceUninstall)
  │       │  Calculate leftovers (cluster entities in namespaces − uninstall entities)
  │       │
  │       ├── SeparateUninstallForceLeftovers(cluster, leftovers, appIds)
  │       │     Split leftovers using uninstall-force CEL predicates
  │       │     → forceLeftovers, untrackedLeftovers
  │       │
  │       ├── If forceLeftovers > 0:
  │       │     WARN "Found N resources that can be force-deleted
  │       │           (use --force to delete, --keep to keep, --force-all to delete all leftovers):"
  │       │       * v1/Secret/ns/name
  │       │       * v1/PersistentVolumeClaim/ns/name
  │       │
  │       ├── If untrackedLeftovers > 0:
  │       │     --force-all → add all leftovers (force + untracked) to deletion set, proceed
  │       │     otherwise   → ERROR abort with hint about --force-all
  │       │
  │       └── If only forceLeftovers (no untracked):
  │             --force     → add force entities to uninstall set, proceed
  │             --force-all → add force entities to uninstall set, proceed
  │             --keep      → ignore force entities, proceed
  │             neither     → abort with hint about --force, --keep, and --force-all
  │       → updated uninstall entities
  │
  ├── 6. RemoveUninstallFinalizers(h, clusterEntities, appIds)
  │       Load uninstall-finalizer names from Hydra values
  │       Scan ALL cluster entities (cluster-wide)
  │       Patch matching resources to strip listed finalizers
  │       Respects --dry-run
  │
  ├── 7. ColoredUninstallMessage()
  │       Display comparison results (selected vs live entities)
  │
  └── 8. DeleteResources(h, entities, key, forceScaleDown, scaleTimeout, phaseOffset=1, totalPhases=3)
         │  The caller passes phaseOffset and totalPhases so DeleteResources
         │  continues the overall phase numbering.
         │
         ├── Phase {phaseOffset}/{totalPhases}: Delete webhook configurations
         │     SplitWebhooks(entities) → webhookEntities, rest
         │     k8s.DeleteWebhookConfigs(ctx, l, dynamicClient, webhookEntities, key, dryRun)
         │     Removes webhook configs before scale-down to prevent admission webhooks
         │     from blocking subsequent API operations.
         │
         ├── Phase {phaseOffset+1}/{totalPhases}: Scale down workloads using TopologicalExecute
         │     TopologicalExecute(ctx, l, entities, ReverseRefs(refs), scaleDown, waitScaledDown)
         │     Dependents are scaled down before their dependencies (reversed edges).
         │     Workloads at the same level scale down concurrently.
         │     On timeout without --force-scale-down: abort (ErrScaleDownTimeout)
         │     On timeout with --force-scale-down: force-delete pods (grace-period=0)
         │
         └── Phase {phaseOffset+2}/{totalPhases}: Foreground deletion (up to 3 passes)
               TopologicalExecute(ctx, l, entities, ReverseRefs(refs), deleteEntity, waitDeleted)
               Remove finalizers → delete with DeletePropagationForeground
```text

### Unit Tests

Test scenarios for the uninstall-force feature:

1. Leftovers only force-deletable + `--force` → proceed, force entities added to deletion set
2. Leftovers only force-deletable + `--keep` → proceed, force entities ignored
3. Leftovers only force-deletable + no flag → abort with hint about `--force` and `--keep`
4. Untracked leftovers present + `--force` → abort (untracked always blocks)
5. Mixed untracked + force-deletable → abort
6. `--keep` without force leftovers → no effect, no error, proceed normally
7. `--force` + `--keep` together → mutual exclusion error
8. No leftovers at all → normal proceed, flags have no effect
9. Untracked leftovers + `--force-all` → proceed, all leftovers (force + untracked) added to deletion set
10. Mixed force + untracked leftovers + `--force-all` → proceed, all added to deletion set
11. `--force-all` + `--keep` together → mutual exclusion error
12. `--force-all` + `--force` together → mutual exclusion error
13. No leftovers + `--force-all` → normal proceed, flag has no effect
14. Leftovers only force-deletable (no untracked) + `--force-all` → proceed, force entities added to deletion set

### Unit Tests (uninstall-finalizer)

Test scenarios for the `collectFinalizerPatches` pure function:

1. No finalizers configured → no patches returned
2. No entities have matching finalizers → no patches returned
3. Entity has only a matching finalizer → patch with empty `FinalizersToKeep` list
4. Entity has matching + non-matching finalizers → patch keeps only the non-matching ones
5. Entity has multiple matching finalizers → all matching finalizers removed in one patch
6. Multiple entities match → one `FinalizerPatch` per entity
7. Empty entity list → no patches returned
