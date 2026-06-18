# Commands: Rendering and Listing

This file covers rendering, scope information, live listing, and namespace-related command details.

Back to [Commands detail index](../commands.md).

## Rendering Commands

### RenderCluster

```go
func RenderCluster(
    cluster *hydra.Cluster,
    appIds sets.Set[types.AppId],
    kubernetesVersion types.KubernetesVersion,
    crdMode types.CrdMode,
    skipRootApps types.SkipRootApps,
    requiredGVKs ...sets.Set[types.GVKString],
) (entity.Entities, sets.Set[types.Namespace], sets.Set[types.AppId], error)
```text

Renders all (or selected) apps in a cluster to entities.

**`requiredGVKs` pass-through:** The `requiredGVKs` parameter is variadic for backward compatibility. When provided, it is forwarded to `ApplyScopeInfoMap` inside `RenderCluster`. The `clusterScale` action uses this to ensure custom workload CRDs are present:

```text
clusterScale action
  → HydraAppScaleWorkloads → customWorkloads → extract GVK keys → requiredGVKs
  → RenderCluster(cluster, appIds, ..., CrdModeIgnoreOptional, skipRootApps, requiredGVKs)
  → internally: ScopeInfoMapFromCluster passes requiredGVKs
  → internally: ApplyScopeInfoMap(CrdModeIgnoreOptional, ..., requiredGVKs)
```

Renders all (or selected) apps in a cluster to entities:

```text
Cluster
  │
  ▼
1. Get all root apps in cluster
  │
  ▼
2. For each root app:
  │  a. Get child app IDs
  │  b. Filter by appIds (if specified)
  │  c. Template each app → YAML manifest
  │  d. NewEntitiesFromYaml → entities
  │  e. enrichEntityPaths → set RepoPath + AbsPath on each entity
  │
  ▼
3. Apply scope info (namespaced flag, namespace)
  │  ApplyScopeInfoMap also propagates the resolved namespace to each
  │  entity's Unstructured object. For namespaced entities whose
  │  Unstructured lacks metadata.namespace, the resolved namespace
  │  (from entity.Namespace() / AppNamespace) is written into the
  │  Unstructured's metadata.namespace. This ensures that ToYaml
  │  serializes the correct namespace for kubectl apply -f -.
  │
  ▼
4. Merge all entities into single collection
  │
  ▼
Entities
```text

### Template patches and synthetic `kubernetes-defaults` entities

[`RenderCluster`](../../commands.md) (and `hydra gitops template`, `hydra gitops diff`, `hydra gitops apply`, etc.) may append synthetic manifests whose `# Source:` path is `kubernetes-defaults-<namespace>.yaml` (`v1/Namespace`, namespaced `ServiceAccount/default`, `ConfigMap/kube-root-ca.crt`). Those entities are parsed like Helm output but initially carry **no** template `appIds`.

When `global.hydra.templatePatches` run, [`BuildTemplatePatchOwnerByNamespace`](../../commands.md) supplies the same **namespace → owning app** map as clone resolution (`HydraAppNamespaceOwners` + [`BuildNamespaceOwnerMap`](../../commands.md) over the merged full-cluster template catalog). The patch pipeline uses it in two ways:

1. **Declaring-app rules on synthetics:** a rule with non-empty `declaringApp` is evaluated for a synthetic entity only if that `declaringApp` equals the resolved owner for the resource’s namespace (for `v1/Namespace` objects, `metadata.name` is the namespace key).
2. **Attribution after a mutating patch:** if the YAML actually changes, Hydra sets exactly **one** template `appId` on that entity so it is included in app-scoped views—either the common `declaringApp` among rules that performed yq, or (if only global rules ran) the namespace owner. Unchanged synthetics keep **no** `appIds`.

[`hydra local template`](../../../../manual/cli/local/hydra-template.md) does not append this bundle; [`ApplyTemplatePatchesUsingPartitionRender`](../../commands.md) still merges patch rules from the Git scope catalog for local renders.

**Variants:**

| Function                                                                   | Description                                    |
| -------------------------------------------------------------------------- | ---------------------------------------------- |
| `RenderClusterAllApps(cluster, networkMode, k8sVersion, key)`              | Renders all apps in the cluster                |
| `RenderClusterSelectedApps(cluster, networkMode, k8sVersion, appIds, key)` | Renders only apps matching the given AppId set |

### RenderClusterSelectedApps

`RenderClusterSelectedApps` templates the same selected-app subset as the corresponding branch inside `RenderCluster`, enriches paths, and sets `appIds` / `AppNamespace` on entities. **Namespace contract:** it must also run the same scope discovery and `ApplyScopeInfoMaps` step as `RenderCluster` before returning entities to callers such as `ReviewRefs`, `hydra local find`, or any logic that indexes by `Entity.Id()` or resolves references by namespace. Because full cluster scope is not available yet on that early path, the implementation uses an internal `CrdModeKeepUnknown` branch: built-in resources and CRDs defined in the selected render are normalized immediately, while still-unknown GVKs are kept unchanged until later stages have more scope information.

**Why:** `ApplyScopeInfoMap` (via `ApplyScopeInfoMaps`) sets `KeyNamespaced`, resolves `KeyNamespace` from the app when the manifest omits `metadata.namespace`, and copies that value into the template `Unstructured`'s `metadata.namespace` (see [Scope Info](#scope-info)). `RenderCluster` already does this after templating. The selected-apps path must not skip it: if namespaced objects stay without a namespace on the `Unstructured`, `Entity.Id()` serializes with an empty namespace segment (for example `networking.k8s.io/v1/Ingress//rules`), ref endpoints and target lookups disagree with Kubernetes semantics (resources effectively live in the app namespace), and commands such as `hydra local review` / `hydra gitops review` can report spurious `missing target resource` findings.

**Implementation placement:** keep the normalization in shared render orchestration (for example inside `RenderClusterSelectedApps` or a helper both `RenderCluster` and selected-app entrypoints use), not in review-only wrappers, so every consumer of the selected-app entity stream shares one invariant.

### Scope Info

```go
func ScopeInfoMapFromCluster(cluster *hydra.Cluster) (types.ScopeInfoMap, error)
```

Extracts scope information from the live cluster via the Kubernetes discovery API. Returns a `ScopeInfoMap` that maps each GVK to its scope (namespaced or cluster-scoped).

```go
func ApplyScopeInfoMap(crdMode types.CrdMode, entities entity.Entities, scopeInfoMap types.ScopeInfoMap, key types.EntityKeyUnstructured) (entity.Entities, error)
```text

Applies scope info to entities, setting the `KeyNamespaced` flag. This is important for correctly handling cluster-scoped resources like ClusterRoles and CRDs.

**Namespace propagation to Unstructured:** For namespaced entities whose Unstructured object lacks `metadata.namespace`, `ApplyScopeInfoMap` copies the resolved namespace (from `entity.Namespace()` / `AppNamespace`) into the Unstructured object's `metadata.namespace`. This ensures that when `ToYaml` serializes the Unstructured for `kubectl apply -f -`, the correct namespace is present in the YAML. Without this, resources from Helm charts that omit `metadata.namespace` would silently land in the Kubernetes "default" namespace instead of the app-configured namespace.

The `key types.EntityKeyUnstructured` parameter is used to access the Unstructured data on each entity for reading and updating `metadata.namespace`.

**Note:** `ApplyScopeInfoMaps` (the plural variant that applies multiple scope info maps) also accepts the `key` parameter and delegates to `ApplyScopeInfoMap`.

**Phase 4 (Scale Up) is not affected** by this bug because `CollectScaleTargets` reads the namespace from `entity.Namespace()` and uses the Kubernetes dynamic client, which accepts namespace as a separate parameter.

### Cluster apply: CRD sources for scope vs apply eligibility

`hydra gitops apply` combines two related but distinct concerns:

1. **Scope determination** — Build the `ScopeInfoMap` (and any merged scope inputs) so `ApplyScopeInfoMap` can classify each rendered GVK as namespaced or cluster-scoped and normalize namespaces on template objects. For this step, **CustomResourceDefinition** objects must be considered from **all** rendered resources for the cluster (the full cluster catalog render), not only from the app IDs passed to the current apply. That way a custom resource instance rendered for a selected app can still obtain scope metadata from a CRD that is packaged with another app in the same cluster (for example an operator chart versus an instance chart). Synthetic example: a `KafkaUser` manifest selected for apply may resolve scope using a CRD that only appears when a sibling app that installs the Strimzi operator is rendered, even if that operator app is not part of this apply’s `appId` list.

2. **Apply safety (CRD availability)** — The manifests actually applied are still limited to the selected apps. Before applying non-CRD resources, the command must ensure every CRD **required** to admit resources in that selected set is available: the CRD must **either** already exist on the target cluster (and be usable by the apiserver) **or** be included among the CRD objects that this run will apply from the selected apps’ rendered output (typically the CRD apply phase). If a selected custom resource needs a CRD that appears only in a non-selected app’s render and that CRD is **not** live on the cluster, the operation must **fail** — applying the instance would leave the cluster without the matching CRD in the same operation. Default CLI behavior remains `--crd-mode error`; `--crd-mode ignore` keeps its existing meaning (skip entities that still lack resolvable scope) and does not redefine this availability rule.

These rules are product-level invariants; concrete function names and error types may evolve in implementation.

### CrdMode

`CrdMode` controls how `ApplyScopeInfoMap` handles entities whose GVK is not found in the scope info map (i.e., entities using CRDs that the cluster cannot discover, e.g., due to stale or removed API groups).

```go
type CrdMode int

const (
    CrdModeError          CrdMode = iota // abort with ErrMissingScope
    CrdModeIgnore                        // skip entity, log WARN
    CrdModeSilent                        // skip entity, no log (internal only)
    CrdModeIgnoreOptional                // skip unknown, error on required GVKs
    CrdModeKeepUnknown                   // keep unknown GVKs unchanged (internal only)
)
```

| Mode                    | Unknown CRD behavior                    | Discovery warnings | CLI flag                     | Used by                                         |
| ----------------------- | --------------------------------------- | ------------------ | ---------------------------- | ----------------------------------------------- |
| `CrdModeError`          | Abort with `ErrMissingScope`            | WARN               | `--crd-mode error` (default) | `cluster apply`, `cluster diff`, `cluster view` |
| `CrdModeIgnore`         | Skip entity, log WARN per skipped GVK   | WARN               | `--crd-mode ignore`          | User-selectable via CLI                         |
| `CrdModeSilent`         | Skip entity silently (no log)           | DEBUG              | Not exposed as CLI flag      | `cluster uninstall`                             |
| `CrdModeIgnoreOptional` | Skip unknown, error on required GVKs    | DEBUG              | Not exposed as CLI flag      | `cluster scale`                                 |
| `CrdModeKeepUnknown`    | Keep unknown GVK unchanged for later    | DEBUG              | Not exposed as CLI flag      | `RenderClusterSelectedApps`                     |

**Discovery warnings:** When `ServerPreferredResources()` returns partial `ErrGroupDiscoveryFailed` errors (stale API groups), `VisitResources` logs the affected groups. The log level depends on the `quiet` parameter: `CrdModeSilent` and `CrdModeIgnoreOptional` pass `quiet=true` (DEBUG), all other modes pass `quiet=false` (WARN). This is propagated through `RenderCluster` → `ScopeInfoMapFromCluster` → `VisitResources`.

**Why `CrdModeSilent` for uninstall:** Uninstall discovers cluster resources via `ListClusterAll` and uses ArgoCD tracking annotations for selection, so skipped template entities do not affect correctness.

**Why `CrdModeIgnoreOptional` for scale:** Scale operations need built-in workloads (Deployment, StatefulSet, DaemonSet, ReplicaSet) plus any custom scale workloads declared via `global.hydra.scale`. Built-in workloads are always in the default scope. Custom workloads (e.g., Strimzi Kafka CRs) must be present — their CRDs are required. Other CRD-based entities (e.g., `KafkaTopic`) are irrelevant and can be silently skipped. `CrdModeIgnoreOptional` combines both behaviors: GVKs declared in `global.hydra.scale` (passed as `requiredGVKs`) cause an error if missing; all other unknown GVKs are silently skipped.

**Why `CrdModeKeepUnknown` for selected-app render:** The early selected-app render path must already normalize built-in namespaced resources so `Entity.Id()` and reference lookups use the app namespace, but it does not yet have the merged live-cluster scope map that `RenderCluster` gets later. `CrdModeKeepUnknown` lets that path normalize what it can from the default scope map plus rendered CRDs without dropping still-unknown GVKs prematurely.

**`ApplyScopeInfoMap` signature with `requiredGVKs`:**

```go
func ApplyScopeInfoMap(
    crdMode types.CrdMode,
    entities entity.Entities,
    scopeInfoMap types.ScopeInfoMap,
    key types.EntityKeyUnstructured,
    requiredGVKs ...sets.Set[types.GVKString],
) (entity.Entities, error)
```text

When `crdMode == CrdModeIgnoreOptional`:

- GVK in ScopeInfoMap → processed normally
- GVK not in ScopeInfoMap AND not in `requiredGVKs` → silently skipped (like `CrdModeSilent`)
- GVK not in ScopeInfoMap BUT in `requiredGVKs` → error: `ErrRequiredCrdMissing` (_"CRD for {gvk} is required by global.hydra.scale but not found in cluster"_)

The error type `ErrRequiredCrdMissing` is defined in `base/errors/errors.go` for the case when a required GVK from `global.hydra.scale` is not found in the cluster.

The `requiredGVKs` parameter is variadic to maintain backward compatibility. When not provided or empty, `CrdModeIgnoreOptional` behaves identically to `CrdModeSilent`.

### Unit Tests (ApplyScopeInfoMap — Namespace Propagation)

Test file: `core/commands/scope_info_test.go`

1. Entity has AppNamespace but Unstructured lacks `metadata.namespace` — after `ApplyScopeInfoMap`, the Unstructured's `metadata.namespace` is set to the entity's resolved namespace
2. Entity already has a namespace in the Unstructured `metadata.namespace` — the existing namespace remains unchanged (not overwritten)
3. Cluster-scoped entity (e.g. ClusterRole) — namespace is NOT propagated to the Unstructured
4. Entity without Unstructured data for the given key — handled gracefully, entity returned unchanged

### Unit Tests (ApplyScopeInfoMap — CrdModeIgnoreOptional)

Test file: `core/commands/scope_info_test.go`

1. GVK in ScopeInfoMap → processed normally (entity returned with scope info applied)
2. GVK not in ScopeInfoMap, not in `requiredGVKs` → silently skipped (entity filtered out, no error)
3. GVK not in ScopeInfoMap, IS in `requiredGVKs` → returns error (_"CRD for {gvk} is required by global.hydra.scale but not found in cluster"_)
4. No `requiredGVKs` provided → behaves like `CrdModeSilent` (all unknown GVKs silently skipped)

### Unit Tests (Cluster apply — CRD scope merge and eligibility)

1. **Full-cluster CRD merge for scope** — With a fixture where two apps render distinct manifests and the CRD for GVK `example.com/v1/Example` exists only in the non-selected app’s output, assert that the scope path used by `cluster apply` still maps that GVK in the scope info map when classifying entities from the selected app (namespaced vs cluster-scoped and namespace propagation behave as if the CRD were known).
2. **Eligibility failure** — Same fixture, mocked cluster without that CRD established: assert `cluster apply` for only the app that renders `Example` instances fails because the CRD is neither live nor part of the selected apply’s CRD set.
3. **Eligibility success (CRD in selection)** — Assert success when the selected apps’ rendered CRDs include the definition, or when the mocked cluster already has the CRD established.

Prefer table-driven tests with small YAML fragments over coupling to a specific third-party API group in assertions.

## Listing Commands

### ListCluster

```go
func ListCluster(
    cluster *hydra.Cluster,
    key types.EntityKeyUnstructured,
    eval func(entity.Entity, types.MissingKeys) (bool, error),
) (entity.Entities, error)
```

Generic cluster listing function. Fetches all resources from the live cluster via `VisitResources` and applies a custom evaluation function to filter entities.

### ListClusterPredicate

```go
func ListClusterPredicate(
    cluster *hydra.Cluster,
    key types.EntityKeyUnstructured,
    predicate cel.Predicate,
) (entity.Entities, error)
```text

Lists cluster resources matching a CEL predicate. Used by `hydra gitops dump`.

### ListClusterAll

```go
func ListClusterAll(cluster *hydra.Cluster, key types.EntityKeyUnstructured) (entity.Entities, error)
```

Lists all resources from the live cluster. Returns the full entity collection for further processing (e.g. comparison with rendered templates).

### VisitResources

```go
func VisitResources(cluster *hydra.Cluster, key types.EntityKeyUnstructured, handlers *VisitorHandlers) error
```text

Generic visitor pattern for iterating over all cluster resources using handler callbacks for different resource types (cluster-scoped, namespaced, items).

#### Discovery Cache

`VisitResources` calls `discoveryClient.ServerPreferredResources()` to enumerate all API groups and resources in the cluster. This is an expensive Kubernetes API call. During operations like `cluster uninstall`, `VisitResources` is called twice:

1. Via `ScopeInfoMapFromCluster` (scope_info.go) — reads API group metadata only (no items)
2. Via `ListClusterAll` (list_cluster.go) — reads API groups AND fetches items

Both calls create their own discovery client and would each call `ServerPreferredResources()`, resulting in duplicate API requests and duplicate warning logs for stale API groups (partial `ErrGroupDiscoveryFailed` errors).

**Solution:** A package-level discovery cache in `visit.go` caches the result of `ServerPreferredResources()`. On the second call with the same cluster identity, the cached result is returned — no duplicate API request, no duplicate warnings.

**Signature change:** `VisitResources` narrows its first parameter from `hydra.Hydra` (interface) to `*hydra.Cluster` so that `ContextPath` and `ClusterName` are directly accessible for the cache key. All existing callers already pass `*hydra.Cluster`, so this is a non-breaking change.

**Cache pattern:** Follows the existing `cache.NewCache[K, V]()` pattern used by `clusterCache` (cluster.go), `rootAppCache` (root_app.go), `contextCache` (context.go), and `valuesCache`/`templatesCache` (caches.go).

**Cache key:**

```go
type discoveryCacheKey struct {
    contextPath types.ContextPath
    clusterName types.ClusterName
}
```

Implements `CacheKey` interface (`comparable` + `String() string`). The key uniquely identifies a cluster so discovery results from different clusters are never mixed.

**Cache value:**

```go
type discoveryResult struct {
    apiResourceLists []*metav1.APIResourceList
    partialErr       error
}
```text

The `partialErr` field stores the `*discovery.ErrGroupDiscoveryFailed` error returned by `ServerPreferredResources()`. This partial error contains warnings about stale API groups. By caching it alongside the resource lists, the warnings are only logged on the first call (cache miss). On subsequent calls (cache hit), the cached lists are returned silently.

**Implementation:**

```go
var discoveryCache *cache.Cache[discoveryCacheKey, discoveryResult]

func initDiscoveryCache(l log.Logger) {
    // lazy init with nil check, quiet: true (like clientCache)
}
```

In `VisitResources`, after creating the `discoveryClient`, the function calls `discoveryCache.GetOrLoad(key, loader)`. The loader distinguishes two error paths:

- **Partial error** (`*discovery.ErrGroupDiscoveryFailed`): `ServerPreferredResources()` returns valid resource lists alongside this error. The loader logs the warnings, stores the lists and the partial error in `discoveryResult`, and returns `nil` to `GetOrLoad`. This way the entry is cached as a successful value and the warnings are only logged on the first call (cache miss).
- **Fatal error** (e.g., network failure, unauthorized): `ServerPreferredResources()` returns a non-partial error. The loader returns this error to `GetOrLoad`, which caches it in `cacheEntry.err`. Subsequent calls for the same key return the cached error without calling the loader again.

On cache hit, the loader is not called — neither API request nor warning logs are repeated.

**What is NOT cached:** The dynamic client, handler callbacks, and item listing remain uncached. Only the discovery result (API group metadata) is cached.

#### Unit Tests (Discovery Cache)

Test scenarios for the discovery cache behavior:

1. First call for a cluster — loader executes, `ServerPreferredResources()` called, result cached
2. Second call for the same cluster — loader NOT executed, cached result returned
3. Different cluster (different contextPath or clusterName) — loader executes independently, separate cache entry
4. `ServerPreferredResources()` returns partial `ErrGroupDiscoveryFailed` — error cached in `partialErr`, warnings logged only on first call
5. `discoveryCacheKey.String()` returns a human-readable representation containing contextPath and clusterName
6. `ServerPreferredResources()` returns a fatal (non-partial) error — error returned by `GetOrLoad`, second call returns cached error without calling loader again

## Namespace Commands

### ExclusiveNamespaces

```go
func ExclusiveNamespaces(l log.Logger, entities entity.Entities, selectedAppIds sets.Set[types.AppId]) (sets.Set[types.Namespace], error)
```text

Finds namespaces that are used **exclusively** by the specified apps (not shared with other apps). These namespaces can be safely deleted during uninstall. System namespaces (`kube-system`, `kube-public`, `kube-node-lease`) are always excluded.

### CreateNamespaceEntities

```go
func CreateNamespaceEntities(namespaces sets.Set[types.Namespace]) (entity.Entities, error)
```

Creates namespace entities with their default resources (default ConfigMap and default ServiceAccount). Used when namespaces need to be explicitly managed.

Current uses:

- uninstall helpers that need canonical namespace manifests
- apply phase-plan namespace preparation, where selected app namespaces may be created early so scoped backup restore has valid restore targets before the restore phase starts

### GroupNamespacesByApp

```go
func GroupNamespacesByApp(entities entity.Entities) map[types.Namespace]sets.Set[types.AppId]
```text

Groups namespaces by which apps use them. Returns a map from Namespace to the set of AppIds that have resources in that namespace.
