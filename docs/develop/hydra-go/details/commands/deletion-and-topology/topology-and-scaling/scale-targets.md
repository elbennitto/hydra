# Deletion and Topology: Scale Targets

This page covers custom scale workload definitions, shared scale target discovery, and the related test coverage used by delete, apply, and scale flows.

Back to [Topology and Scaling](../topology-and-scaling.md).

## HydraAppScaleWorkloads

```go
func HydraAppScaleWorkloads(
    cluster *Cluster,
    appIds sets.Set[types.AppId],
    networkMode types.HelmNetworkMode,
) (map[types.GVKString]types.HydraScaleGroup, error)
```

Collects `global.hydra.scale` definitions from all apps' hydra values and returns a map keyed by GVK string. When multiple apps define the same GVK, the first occurrence wins (analogous to `MergeScopeInfoMaps`).

Returns an empty map (not nil) when no apps have scale definitions. Returns an error if any app's hydra values fail to load.

`HydraAppScaleWorkloads` collects `global.hydra.scale` definitions from all apps' hydra values. Each entry maps a GVK string to a `HydraScaleGroup` containing replica paths. Used by `cluster scale` to discover custom CRD-based resources (e.g., Strimzi Kafka CRs) that should be included as scale targets alongside built-in workloads.

For the surrounding `HydraValues` and `HydraScaleGroup` type definitions, see [Uninstall Selection](../uninstall-selection.md#types).

## Ready rules collection (global.hydra.ready)

**Concept:** Scale status and scale-up gating load **`global.hydra.ready`** from merged Helm values the same way as `refs` and `scale`—through the Hydra values hierarchy into `HydraValues` (or an equivalent accessor). Implementation provides a helper analogous in spirit to `HydraAppScaleWorkloads`: merge or collect named ready rules from all contributing apps so the CLI has a single map **`ruleName → { predicate, cel[] }`** before evaluating entities.

**Use:**

- **`hydra gitops scale status`** — For each reported entity that participates in ready display, find the winning rule, evaluate `cel[]`, emit `ready` / `not_ready` or omit when no rule matches. Correlated **Kubernetes Events** appear in `readyMessages` only when **`global.hydra.ready`** expressions return them (for example via **`involvedObjectEvents`** or **`clusterEntities()`** over the merged live inventory); there is no separate post-pass Event list against the API.
- **`ScaleUpWorkloads` / `TopologicalExecute`** — Before advancing dependents, ensure transitive dependencies that **have** a ready rule satisfy **all** `cel` checks (per the `string` / `list(string)` / `null` contract in [details/cel.md](../../../cel.md#global-hydra-ready-rules-predicate-and-cel-list); in addition to existing replica / `statusReadyPath` waits).

**Built-in defaults** are merged with user YAML so built-in workload kinds always have a rule unless explicitly overridden by a same-key or predicate design choice—see [values.md — Built-in default ready rules](../../../values.md#built-in-default-ready-rules).

### Unit Tests (HydraAppScaleWorkloads)

Test file: `core/hydra/hydra_values_test.go`

| Test                                            | Verifies                                                                           |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- |
| `TestHydraAppScaleWorkloads_NoScaleDefinitions` | Apps without `global.hydra.scale` → returns empty map                              |
| `TestHydraAppScaleWorkloads_SingleApp`          | App with scale definitions → returns map with correct GVK→HydraScaleGroup mappings |
| `TestHydraAppScaleWorkloads_MultipleApps`       | Multiple apps with different GVKs → all included                                   |
| `TestHydraAppScaleWorkloads_DuplicateGVK`       | Two apps define same GVK → first occurrence wins                                   |
| `TestHydraAppScaleWorkloads_NilHydraValues`     | App with nil hydra values → skipped, no error                                      |

## Scale Target Collection

`DeleteResources`, `ScaleUpWorkloads`, `ScaleDownWorkloads`, and `LogStartupOrder` all depend on shared scale target discovery for built-in and custom workloads.

**Pure function for testability:**

```go
type ScaleTarget struct {
    Id               types.Id
    Name             types.Name
    Ns               types.Namespace
    GVR              types.GVR
    GVK              types.GVKString
    Replicas         int64              // original replicas from rendered template (0 for DaemonSets)
    IsDaemonSet      bool
    IsJob            bool               // batch/v1 Job (suspend/unsuspend; excluded when owned by CronJob)
    NodeSelector     map[string]string  // full nodeSelector from rendered template (DaemonSets only; authoritative restore target)
    IsCustomWorkload bool               // true for custom CRD-based scale targets from global.hydra.scale
    ReplicaPaths     []string           // dot-separated paths to replica fields (custom workloads only)
    OriginalReplicas map[string]int64   // original values for each ReplicaPath from rendered template
}

func collectScaleDownTargets(entities entity.Entities, key types.EntityKeyUnstructured, customWorkloads ...map[types.GVKString]types.HydraScaleGroup) ([]ScaleTarget, error)
```

This unexported pure function lives in `core/commands/delete.go` and is called by `DeleteResources` to identify workloads needing scale-down. A separate exported function `CollectScaleTargets` with an extended signature lives in `core/commands/scale.go` and is used by `ScaleDownWorkloads`, `ScaleUpWorkloads`, and `LogStartupOrder`. Both functions exist independently — `CollectScaleTargets` did **not** replace `collectScaleDownTargets`. `collectScaleDownTargets` now also supports custom workloads, matching the behavior of `CollectScaleTargets`. Both functions support built-in AND custom workloads everywhere scale up/down is used: explicit `scale` command, `standardApply`, `bootstrapApply`, and `DeleteResources`.

`CollectScaleTargets` accepts an optional `customWorkloads` parameter (variadic for backward compatibility):

```go
func CollectScaleTargets(
    entities entity.Entities,
    key types.EntityKeyUnstructured,
    customWorkloads ...map[types.GVKString]types.HydraScaleGroup,
) ([]ScaleTarget, error)
```

When `customWorkloads` is provided, `CollectScaleTargets` matches entities against custom GVKs in addition to the built-in workload GVKs. For custom workload matches:

- Reads each path from `ReplicaPaths` using `values.Lookup`
- Stores original values in `OriginalReplicas`
- Sets `IsCustomWorkload = true`
- If a `replicaPath` is missing in the entity, defaults to 0 for that path

**Owner-managed workload filtering:** For cluster entities (`KeyClusterEntity`), workloads whose `ownerReferences` point to another workload type (Deployment, StatefulSet, DaemonSet) are filtered out. This primarily affects ReplicaSets owned by Deployments — scaling a Deployment automatically handles its ReplicaSets, so including them as separate scale targets would be redundant. The helper function `isOwnedByWorkload(u *unstructured.Unstructured) bool` checks whether any `ownerReference` on the unstructured object has `Kind` in `{Deployment, StatefulSet, DaemonSet}`. Template entities (`KeyTemplateEntity`) do not carry `ownerReferences` from the cluster, so no filtering is applied to them (this is acceptable because ReplicaSets rarely appear in Helm templates directly).

**Operator CR StatefulSet skip with live fallback:** When the primary key is `KeyTemplateEntity`, `ShouldSkipOperatorManagedStatefulSet` cannot find `ownerReferences` (those are set at runtime, not in rendered templates). The internal wrapper `shouldSkipOperatorStatefulSet` handles this by falling back to the live cluster view (`KeyClusterEntity`) on the same entity. This ensures that StatefulSets managed by operator CRs (e.g. Prometheus Operator creating StatefulSets from Prometheus CRs) are correctly skipped in favor of scaling the CR, even when `CollectScaleTargets` or `ZeroWorkloads` use template-key iteration.

**Jobs:** `batch/v1` Job resources are scale targets when they are not owned by a `CronJob` (`isOwnedByCronJob`). `ZeroWorkloads` sets `spec.suspend: true` so Jobs are not applied in a running state during the scale-zero phase; `ScaleUpWorkloads` unsuspends them in dependency order and waits until the Job completes successfully (or aborts on failure). Scale-down suspends Jobs again and waits until `status.active` is zero.

### Unit Tests (collectScaleDownTargets and CollectScaleTargets)

Test scenarios shared by both `collectScaleDownTargets` (in `delete_test.go`) and `CollectScaleTargets` (in `scale_test.go`):

1. Deployment with replicas=3 — returns `ScaleTarget` with `Replicas: 3`, `IsDaemonSet: false`
2. StatefulSet with replicas=0 — included (scale-up needs to know about it)
3. DaemonSet with nodeSelector — returns `ScaleTarget` with `IsDaemonSet: true`, `NodeSelector` from template
4. DaemonSet without nodeSelector — returns `ScaleTarget` with `IsDaemonSet: true`, `NodeSelector: nil` (scale-up will remove nodeSelector from live resource)
5. ConfigMap — not included
6. Multiple workloads — all returned
7. Entity without unstructured data — skipped
8. `TestCollectScaleDownTargets_ReplicaSetOwnedByDeployment` (in `delete_test.go`) — ReplicaSet with `ownerReference` pointing to a Deployment is excluded from the result when using `collectScaleDownTargets`
9. `TestCollectScaleDownTargets_BareReplicaSet` (in `delete_test.go`) — ReplicaSet without `ownerReferences` is included as a scale target when using `collectScaleDownTargets`
10. `TestCollectScaleTargets_ReplicaSetOwnedByDeployment` (in `scale_test.go`) — same filtering verified via the exported `CollectScaleTargets` entry point
11. `TestCollectScaleTargets_BareReplicaSet` (in `scale_test.go`) — bare ReplicaSet is included via `CollectScaleTargets`

### Unit Tests (CollectScaleTargets — Custom Workloads)

Test file: `core/commands/scale_test.go`

1. Custom workload (Kafka CR with `spec.kafka.replicas=3`, `spec.zookeeper.replicas=3`) → returns `ScaleTarget` with `IsCustomWorkload: true`, `ReplicaPaths: ["spec.kafka.replicas", "spec.zookeeper.replicas"]`, `OriginalReplicas: {"spec.kafka.replicas": 3, "spec.zookeeper.replicas": 3}`
2. Custom workload with single `replicaPath` → works correctly with one path in `ReplicaPaths` and `OriginalReplicas`
3. Custom workload with missing `replicaPath` in entity → defaults to 0 for that path in `OriginalReplicas`
4. Mix of built-in and custom workloads → both included in result (built-in with `IsCustomWorkload: false`, custom with `IsCustomWorkload: true`)
5. Entity matching custom GVK but without unstructured data → skipped
6. No `customWorkloads` parameter → behaves exactly as before (backward compatible, only built-in workloads matched)
