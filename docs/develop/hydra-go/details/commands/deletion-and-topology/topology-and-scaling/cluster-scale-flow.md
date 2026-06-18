# Deletion and Topology: Cluster Scale Flow

This page covers the `cluster scale` command flow, startup-order logging, scale up/down execution on shared topology helpers, and **planned** pod reconciliation after scale (entity refresh, template vs operator-owned pods).

Back to [Topology and Scaling](../topology-and-scaling.md).

## Data Flow (Cluster Scale)

### Command

```text
hydra gitops scale up <appId> [appId...] [flags]
hydra gitops scale down <appId> [appId...] [flags]
```

**Subcommands:**

| Command                                | Description                                 |
| -------------------------------------- | ------------------------------------------- |
| `hydra gitops scale up <appIds...>`   | Scale up workloads, supports `*` wildcard   |
| `hydra gitops scale down <appIds...>` | Scale down workloads, supports `*` wildcard |

**Flags:** `--hydra-context`, `--dry-run`, `--network-mode`, `--kubernetes-version`, `--force-scale-down` (only for `down`: see [Pod reconciliation](#pod-reconciliation-after-scale-planned)), `--scale-timeout` (default `10m`: polling timeout for workload readiness)

### LogStartupOrder

```go
func LogStartupOrder(l log.Logger, entities entity.Entities, refs []types.Ref, key types.EntityKeyUnstructured, customWorkloads ...map[types.GVKString]types.HydraScaleGroup) ([]PlanEntry, error)
```

Pure helper that computes and logs the startup order (scale-up order) without any cluster interaction. Used by `hydra gitops apply --no-cluster` to display the planned scale-up order before returning.

**Steps:**

1. `CollectScaleTargets(entities, key, customWorkloads)` — find workload entities (built-in: Deployment, StatefulSet, ReplicaSet, DaemonSet; custom: from `global.hydra.scale`)
2. `filterWorkloadEntities(entities, targets)` — filter to workload entities only
3. `ResolveTransitiveWorkloadDeps(refs, workloadIds)` — trace through non-workload intermediaries and preserve tags such as `optional:startup`
4. Split workload refs into required vs optional planning sets
5. `BuildDependencyGraph(...)` + `PlanTopologicalOrder(...)` for the required set
6. `BuildDependencyGraph(...)` + `PlanTopologicalOrder(...)` for optional-only workloads
7. `logScaleUpPlan(l, ...)` — display the combined startup order (required part first, optional part second)

**Output format** (same as `ScaleUpWorkloads` plan display):

```text
scale-up order:
  1. workload-a (no dependencies)
  2. workload-b (no dependencies)
  3. workload-c (after: workload-a)
  4. workload-d (after: workload-a, workload-b)
```

**Unit tests (LogStartupOrder):** Test file: `core/commands/scale_test.go`

| Test                                       | Verifies                                                                 |
| ------------------------------------------ | ------------------------------------------------------------------------ |
| `TestLogStartupOrder_EmptyEntities`        | Empty entities → no log output (logScaleUpPlan skips when plan is empty) |
| `TestLogStartupOrder_NoWorkloads`          | Entities with only non-workloads (ConfigMap, Service) → no log output    |
| `TestLogStartupOrder_SingleWorkload`       | Single workload → logs "1. name (no dependencies)"                       |
| `TestLogStartupOrder_IndependentWorkloads` | Multiple workloads with no refs → correct order, all "(no dependencies)" |
| `TestLogStartupOrder_WithDependencies`     | Workloads with refs → correct topological order with "(after: ...)"      |
| `TestLogStartupOrder_MixedEntities`        | Workloads + ConfigMaps + Services → only workloads appear in output      |

**Integration:** `hydra gitops apply --no-cluster` must display the startup order before the summary and return. No dedicated integration test required if unit tests cover `LogStartupOrder` and the action handler is updated to call it.

### ScaleUpWorkloads

```go
func ScaleUpWorkloads(ctx context.Context, l log.Logger, dynamicClient dynamic.Interface, entities entity.Entities, refs []types.Ref, key types.EntityKeyUnstructured, dryRun types.DryRun, scaleTimeout time.Duration, customWorkloads ...map[types.GVKString]types.HydraScaleGroup) error
```

Scales up workloads using `TopologicalExecute` with dependency-aware ordering. Before executing, `ScaleUpWorkloads` computes and logs a topological plan showing which workloads will be started in which order:

1. Calls `ResolveTransitiveWorkloadDeps(refs, workloadIds)` to trace through non-workload intermediaries and preserve tags such as `optional:startup`
2. Splits workload refs into required vs optional planning sets
3. Calls `BuildDependencyGraph(...)` / `PlanTopologicalOrder(...)` for the required workload set
4. Calls `BuildDependencyGraph(...)` / `PlanTopologicalOrder(...)` for optional-only workloads
5. Logs the combined plan via `l.InfoLog` in the format:

   ```text
   scale-up order:
     1. workload-a (no dependencies)
     2. workload-b (no dependencies)
     3. workload-c (after: workload-a)
     4. workload-d (after: workload-a, workload-b)
   ```

6. Calls `TopologicalExecute` for required workloads first, then for optional-only workloads

This plan display applies to ALL callers of `ScaleUpWorkloads`: `cluster scale up` and the scale-up phase inside `cluster apply`.

The `scaleTimeout` parameter controls the per-workload polling timeout (default `10m`, configurable via `--scale-timeout`).

**Dry-run mode:** When `dryRun` is true, `ScaleUpWorkloads` prepends `[dry-run]` to each action log message (e.g. `[dry-run] scaling up web-app to 2 replicas`, `[dry-run] restoring log-agent: removing nodeSelector`). There is NO separate dry-run log line — the prefix on the action message itself is sufficient. The Kubernetes patch is skipped entirely. Polling (`waitReady`) is also skipped.

**Readiness waiting:** If a workload already reports the desired ready count when `waitReady` starts, `ScaleUpWorkloads` treats it as ready immediately. In that case it must not log `waiting for {name} to become ready` and must not enter an unnecessary readiness wait loop.

**DaemonSet restore semantics:** `ScaleDownWorkloads` disables DaemonSets by writing `{"hydra-gitops.org/hydra-disabled":"true"}` into `spec.template.spec.nodeSelector`. During restore, `ScaleUpWorkloads` must treat `ScaleTarget.NodeSelector` as the full rendered-template source of truth. Restore therefore uses replace semantics, not merge semantics:

- If the rendered template has no `nodeSelector`, remove `spec.template.spec.nodeSelector` from the live DaemonSet.
- If the rendered template has a `nodeSelector`, replace the entire live `spec.template.spec.nodeSelector` map with that template map.
- Do not merge the template selector into the live object, because merge semantics would incorrectly preserve `hydra-gitops.org/hydra-disabled` or any other scale-down-only keys that are absent from the template.

**Required test coverage:** Add or update `ScaleUpWorkloads` unit tests for:

1. Already-ready workloads: verify an already-ready workload is recognized immediately and does not trigger unnecessary waiting or the corresponding waiting log.
2. DaemonSet restore with template selector: start from a live DaemonSet whose `nodeSelector` still contains `hydra-gitops.org/hydra-disabled: "true"` and verify restore replaces the selector with the rendered template selector so the disable key is removed when absent from the template.
3. DaemonSet restore without template selector: verify restore removes `spec.template.spec.nodeSelector` entirely when the rendered template has no selector.

### ScaleDownWorkloads

```go
func ScaleDownWorkloads(ctx context.Context, l log.Logger, dynamicClient dynamic.Interface, entities entity.Entities, refs []types.Ref, key types.EntityKeyUnstructured, dryRun types.DryRun, forceScaleDown types.ForceScaleDown, scaleTimeout time.Duration, customWorkloads ...map[types.GVKString]types.HydraScaleGroup) error
```

Scales down workloads using `TopologicalExecute` with reversed edges. The `scaleTimeout` parameter controls the per-workload polling timeout (default `10m`, configurable via `--scale-timeout`). When the timeout expires and `forceScaleDown` is true, remaining workload pods are force-deleted inside `waitScaledDown` as today. **Additional** pod deletion behavior after the main scale-down pass (app-associated pods discovered on a refreshed entity list) is specified under [Pod reconciliation](#pod-reconciliation-after-scale-planned) and is orchestrated at the `cluster scale down` action layer, not inside `ScaleDownWorkloads` alone.

**Dry-run mode:** When `dryRun` is true, `ScaleDownWorkloads` prepends `[dry-run]` to each action log message (e.g. `[dry-run] scaling down web-app to 0 replicas`, `[dry-run] disabling daemonset log-agent`). There is NO separate dry-run log line — the prefix on the action message itself is sufficient. The Kubernetes patch is skipped entirely. Polling (`waitScaledDown`) is also skipped.

### Scale Up Data Flow

**CRD mode:** The scale command uses `CrdModeIgnoreOptional` when calling `RenderCluster`. Built-in workload entities (Deployment, StatefulSet, DaemonSet, ReplicaSet) are always present in the default scope info. Custom scale workloads declared via `global.hydra.scale` (e.g., Strimzi Kafka CRs) are required — their GVKs are passed as `requiredGVKs`, causing an error if the CRD is missing. All other CRD-based entities (e.g., `KafkaTopic`) are irrelevant and silently skipped.

```text
hydra gitops scale up my-cluster.*
  │
  ▼
1. HydraAppScaleWorkloads(cluster, appIds, networkMode)
   → customWorkloads: map[GVKString]HydraScaleGroup
   → requiredGVKs: set of all GVKs from customWorkloads
  │
  ▼
2. RenderCluster(cluster, appIds, ..., crdMode=CrdModeIgnoreOptional, requiredGVKs)
   → renderedEntities (with git-defined replica counts)
   → Custom CRD entities (Kafka, KafkaConnect) are included (GVK is required)
   → Other CRD entities (KafkaTopic, etc.) are silently skipped
  │
  ▼
3. references.Refs(l, renderedEntities, KeyTemplateEntity)
   → refs
  │
  ▼
4. CollectScaleTargets(entities, key, customWorkloads)
   → Matches built-in GVKs (Deployment, StatefulSet, ReplicaSet, DaemonSet)
   → ALSO matches custom GVKs (Kafka, KafkaConnect, etc.)
   → For custom: reads all replicaPaths, stores original values
   → For DaemonSets: captures the full rendered-template nodeSelector as the restore target
  │
  ▼
5. ResolveTransitiveWorkloadDeps(refs, workloadIds) → enrichedRefs
   Traces non-workload intermediaries (Secrets, SopsSecrets, etc.)
   and provider-based operator connections to create synthetic workload-to-workload refs.
  │
  ▼
6. BuildDependencyGraph(workloadEntities, enrichedRefs) → graph
  │
  ▼
7. PlanTopologicalOrder(graph) → plan
   Log plan via l.InfoLog:
     scale-up order:
       1. workload-a (no dependencies)
       2. workload-b (after: workload-a)
       ...
  │
  ▼
8. TopologicalExecute(ctx, l, workloadEntities, enrichedRefs,
     start = func(ctx, e) {
       Built-in workloads:
         Deployment/StatefulSet/ReplicaSet:
           Patch spec.replicas to rendered template value
         DaemonSet:
           Read live spec.template.spec.nodeSelector
           If template has no nodeSelector: remove the field from the live object
           If template has a nodeSelector: replace the entire live map with the rendered template map
           Do not merge, because scale-down added hydra-gitops.org/hydra-disabled and restore must remove it unless the template also contains it
       Custom workloads:
         Patch each replicaPath to value from OriginalReplicas
     },
     waitReady = func(ctx, e) {
       Poll every 2s (configurable timeout, default 10m via --scale-timeout):
         Built-in:
           Deployment/StatefulSet/ReplicaSet: status.readyReplicas == spec.replicas
           DaemonSet: status.desiredNumberScheduled == status.numberReady
         Custom (with statusReadyPath):
           Check field at statusReadyPath: non-nil/non-empty means ready
         Custom (without statusReadyPath):
           Polling skipped (fire-and-forget, operator handles reconciliation)
       Additionally (global.hydra.ready):
         If the entity matches a ready rule (user-defined or built-in default), evaluate
         all CEL strings in that rule's `cel` list against the current entity (live view).
         All must be true before the workload is considered ready for topological proceed;
         any false blocks until timeout. Entities with no matching rule skip this gate.
         Errors / non-bool CEL results: behavior per architecture (see values.md).
       If timeout: return ErrScaleUpTimeout
     },
   )
   │
   │  Workloads with no dependencies start concurrently.
   │  When a workload becomes ready, dependents whose last
   │  remaining dependency was that workload start immediately.
```

- **(Planned)** After step 8, if the command mutates the cluster, run **pod reconciliation for scale up** (see [Pod reconciliation — scale up](#pod-reconciliation-after-scale-planned)).

### Scale Down Data Flow

```text
hydra gitops scale down my-cluster.*
  │
  ▼
1. HydraAppScaleWorkloads + RenderCluster (same as scale up steps 1-2)
  │
  ▼
2. CollectScaleTargets(entities, key, customWorkloads) + discover refs
  │
  ▼
3. TopologicalExecute(ctx, l, workloadEntities, ReverseRefs(refs),
     start = func(ctx, e) {
       Built-in workloads:
         Deployment/StatefulSet/ReplicaSet: patch spec.replicas = 0
         DaemonSet: patch nodeSelector = {"hydra-gitops.org/hydra-disabled": "true"}
       Custom workloads:
         Patch each replicaPath to 0
     },
     waitReady = func(ctx, e) {
       Poll every 2s (configurable timeout, default 10m via --scale-timeout):
         Built-in:
           Deployment/StatefulSet/ReplicaSet: status.replicas == 0
           DaemonSet: status.currentNumberScheduled == 0
         Custom (with statusReadyPath):
           Check field at statusReadyPath: nil/zero/empty means scaled down
         Custom (without statusReadyPath):
           Polling skipped (fire-and-forget, operator handles reconciliation)
       If timeout: return ErrScaleDownTimeout
     },
   )
   │
   │  Reversed edges: dependents are scaled down before their
   │  dependencies. Workloads at the same level scale concurrently.
```

### Cluster-only workload scale down

- **Step 4 — Cluster-only workload scale-down (`ScaleDownClusterOnlyWorkloads`)** — After step 3, scale built-in workloads (`apps/v1` Deployment, StatefulSet, ReplicaSet, DaemonSet) that appear **only** as live cluster entities (`KeyClusterEntity` present, `KeyTemplateEntity` absent) when they are associated with the selection via transitive `ownerReferences` to a rendered object that has both template and live UIDs (same `entityOwnedByLiveTemplateEntity` / live-template UID notion as app-associated Pods), **or** when a resolved Hydra ref links a **template-anchored** entity (same template+live UID membership) **to** the cluster-only workload id (`To` endpoint after the same `From`/`To` swap as `ref.Reverse` in scale dependency edges). This covers operator-created objects (for example a StatefulSet owned by a `Prometheus` CR that is rendered in Git, or a Zalando Postgres StatefulSet tied to a rendered `postgresql` CR via an explicit ref when the operator does not set `ownerReferences`). Patches mirror template scale-down: `spec.replicas = 0` or DaemonSet nodeSelector disable. **Without** `--force-scale-down`, after successful patches Hydra waits up to `--cluster-workload-timeout` (default `1m`) for pods that list one of the scaled workload UIDs in `ownerReferences` to disappear; if any remain, it logs **WARN** and returns `ErrClusterWorkloadWaitTimeout`. **With** `--force-scale-down`, no wait after this step; pod cleanup continues in **`ReconcileScaleDownPods`**. The CLI rejects using `--force-scale-down` and `--cluster-workload-timeout` together when both flags are explicitly set.

- After step 4, **`ReconcileScaleDownPods`** lists Pods and merges them into the entity set when workloads were mutated **or** when a follow-up pass is needed for app-associated Pods (still running or **terminating** with `metadata.deletionTimestamp` set). Implementation uses the same refresh merge as `refreshAllPods` (strip live unstructured key for Pods, merge listed `v1/Pod` rows). If workloads did not mutate and no such Pods exist, the reconcile returns without further action (after a single list to confirm). The mutation gate for reconcile includes cluster-only scale-down when it applied patches.

- **(Planned)** Scan **all** `v1/Pod` entities on the refreshed list and apply deletion rules:

  - **Direct template pods:** If a Pod’s ID appears as a concrete `v1/Pod` in the rendered templates (`KeyTemplateEntity`), delete the **live** Pod (API delete). This covers static Pod manifests shipped with the chart.
  - **App-associated pods via owner references:** If a live Pod is **not** directly in the template set but is linked **directly or transitively** through `ownerReferences` to a workload or other entity that belongs to the Hydra app selection (same notion as entities carrying app scope / workload membership used elsewhere in scale—implementation uses `RootOwnerUidMap` + app/workload entity UID sets), treat it as **app-associated**. Delete it when policy allows (see below).

- **Default (`--force-scale-down` false):** For app-associated Pods that are still **present** (including **Terminating** when `metadata.deletionTimestamp` is set), log **WARN** only; do **not** delete. Terminating Pods use a distinct WARN message from still-running Pods. After **all** such WARN lines for the run, log **one** hint line that the user may re-run with `--force-scale-down` to delete or force-delete those Pods.

- **With `--force-scale-down`:** `Delete` app-associated Pods with `GracePeriodSeconds: 0` (force-delete). This is **in addition to** the existing timeout-based force delete inside `ScaleDownWorkloads` (workload Pods stuck until scale-down timeout).

**Dry-run:** Pod refresh listing may still run for accuracy of the plan; API **deletes** and API **creates** for pod reconciliation must be skipped when `dryRun` is true, with `[dry-run]`-prefixed logs where appropriate (align with `ScaleUpWorkloads` / `ScaleDownWorkloads` dry-run style).

**Scope:** Pod reconciliation is specified for **`hydra gitops scale up` and `hydra gitops scale down` only**. It is intentionally wired only into the `cluster scale` command path; `hydra gitops uninstall` and `hydra gitops apply` keep their existing orchestration unless they are explicitly connected to the same post-scale hook later.

---

## Pod reconciliation after scale (planned)

This section ties the [entity-level refresh API](../../../entity.md#planned-unstructured-key-hygiene-and-cel-backed-refresh) to cluster scale.

### Shared primitives

| Primitive | Role |
| --------- | ---- |
| Strip unstructured key from all entities | Remove a live view (e.g. `KeyClusterEntity`) before re-listing; drop empty non-template entities |
| `Entities` refresh (CEL + key) | Re-list matching live objects, merge into `Entities` |
| `refreshAllPods` | Refresh with a Pod selector CEL expression and the live unstructured key used by scale |

### Scale down — after workload scale-down

1. List Pods and merge into entities (same refresh as `refreshAllPods`). If workloads did **not** mutate and **no** template-direct or owner-linked Pod still needs action, return without further steps (after one list).
2. Classify each Pod:
   - **Template-direct:** Pod resource exists in template entities → delete live Pod (optionally with `GracePeriodSeconds: 0` when `--force-scale-down` is set).
   - **Owner-linked to app/workload:** transitive owner chain reaches an app-scoped template entity → subject to warn vs force-delete policy below (including **Terminating** Pods).
   - **Other:** leave unchanged (cluster components, unrelated operators, etc.).
3. Apply `--force-scale-down` policy for owner-linked Pods (all WARNs first, then one hint line vs delete with `GracePeriodSeconds: 0`).

### Scale up — after workload scale-up

1. Detect whether the scale-up execution actually changed the cluster (any successful patch). If **dry-run** or **no mutation**, skip pod reconciliation for creates (same gate as scale down). When mutation occurred, refresh Pods the same way as on scale down (or rely on a merged entity list already updated—implementation may combine steps; the contract is: decisions use **current** template + live Pod entities).
2. **Template-direct `v1/Pod`:** If the template contains a real `v1/Pod` object and the live cluster lacks that Pod (by ID), **create** the Pod by materializing the template `Unstructured` through the same **resource-create path** used elsewhere when applying a manifest object (Server-Side Apply / create helper—whatever the implementation already uses for a single concrete `v1/Pod` document). This is **not** an invocation of the full `hydra gitops apply` phase plan and **does not** imply that `cluster apply` automatically runs this post-scale Pod hook; only the **shape** of the written object matches what apply would produce for that one template Pod. Idempotence: if the Pod already exists, no-op.
3. **Operator / controller Pods:** Pods that only appear because a Deployment, StatefulSet, or custom workload creates them are **not** created directly by Hydra; the controller is expected to create them after replicas are restored. No direct `Pod` create for owner-only Pods.

### Unit tests to add or extend (commands / actions)

| Area | What to cover |
| ---- | ------------- |
| Entity strip + refresh + merge | Covered in `core/entity` (see [details/entity.md — unit tests](../../../entity.md#unit-tests-entity-package-and-merge)); commands tests should use fakes or inject listers where the refresh API is called. |
| `refreshAllPods` | With mocked dynamic client: correct GVR list call, CEL passed through, merge updates Pod entities. |
| Scale down — template Pod | Live Pod exists, template contains same ID → delete invoked. |
| Scale down — owner-linked | Pod owned by Deployment in app → WARN without `--force-scale-down`; with flag → delete. |
| Scale down — hint | Multiple WARNs emit **one** hint line about `--force-scale-down`, **after** the last WARN. |
| Scale down — no mutation | Scale-down no-op (already zero) → no refresh / no pod pass (unless documented exception). |
| Scale up — template Pod | Template has Pod, cluster missing → create. Template has Pod, cluster present → no create. |
| Scale up — operator Pod | Pod only as RS-owned child → no direct Pod create from Hydra. |
| CLI / action wiring | `ClusterScaleDown` / `ClusterScaleUp` pass `ForceScaleDown` and trigger reconciliation only when required. |

**Existing tests** in `core/commands/scale_test.go` (`LogStartupOrder`, `ScaleUpWorkloads`, `ScaleDownWorkloads`, `CollectScaleTargets`, …) remain required; add new test files or tables alongside scale tests for pod reconciliation.

---

**Shared logic:** The scale-up phase inside `ClusterApply` calls the same `ScaleUpWorkloads` function that the standalone `hydra gitops scale up` command uses. Both pass the `scaleTimeout` from the respective flags. **Planned:** only the standalone `hydra gitops scale {up,down}` commands (and code paths they exclusively own) perform the post-scale **Pod** reconciliation described above unless `ClusterApply` is later wired to the same hook explicitly—current docs assume **cluster scale** as the primary scope.

### Scale status: sync vs ready

`hydra gitops scale status` reports **two independent dimensions** per workload node (root targets and dependency children):

1. **Sync** — unchanged: `up` / `down` / `out_of_sync` from template vs live scale fields.
2. **Ready** — present only when a **`global.hydra.ready`** rule matches (including product **defaults**): `ready` vs `not_ready` from the rule’s `cel` list. No matching rule → **omit** ready fields for that entity in text and YAML.

**Ready dependency expansion** uses the **transitive** ref graph (same spirit as inspect commands—BFS through arbitrary entity kinds), filtered to entities that **also** have a matching ready rule, so the status tree does not imply ready semantics for ConfigMaps, Secrets, etc. unless a rule selects them.

Configuration shape and default rules: [values.md](../../../values.md) (`global.hydra.ready`).
