# Apply and Webhooks: Apply Core

This page covers server-side apply behavior and the helper functions that support apply orchestration.

Back to [Apply and Webhooks](../apply-and-webhooks.md).

## ServerSideApply Annotation Support

### ShouldServerSideApply

```go
func ShouldServerSideApply(e entity.Entity, key types.EntityKeyUnstructured) bool
```text

**Source file:** `core/commands/server_side_apply_annotation.go`

Checks if the entity's unstructured data has the annotation `argocd.argoproj.io/sync-options` containing the string `ServerSideApply=true`. Returns `true` if yes.

### ClusterClient

**Source file:** `core/k8s/client.go`

```go
type ClusterClient struct {
    Dynamic    dynamic.Interface
    RESTMapper meta.RESTMapper
}
```

Bundles the Kubernetes dynamic client and REST mapper needed for API operations. Created via `NewClusterClient(configFlags)` which calls `configFlags.ToRESTConfig()` for the dynamic client and `configFlags.ToRESTMapper()` for GVK-to-GVR resolution.

The REST mapper is required because template entities are built via `WithGVK()` (Group/Version/Kind only). The `Resource` field (plural form, e.g. `deployments`) is not set on template entities, so `entity.GVR()` would fail. The REST mapper resolves GVK to GVR by querying the API server's discovery endpoint.

### Updated Apply Function

**Source file:** `core/k8s/apply.go`

```go
func Apply(
    ctx context.Context,
    l log.Logger,
    cc *ClusterClient,
    entities entity.Entities,
    key types.EntityKeyUnstructured,
    dryRun types.DryRun,
    serverSideApply bool,
) (string, error)
```text

Applies Kubernetes resources using the Go client-go API instead of shelling out to `kubectl`. Accepts `entity.Entities` directly (no YAML serialization needed). Uses `ClusterClient.RESTMapper` to resolve each entity's GVK to a GVR.

**Error handling:** Fail-fast — the function returns immediately on the first entity that fails to apply, consistent with the existing behavior where `kubectl apply -f -` aborts on the first error.

**DryRun:** Uses server-side dry run via `CreateOptions{DryRun: []string{metav1.DryRunAll}}` and `PatchOptions{DryRun: []string{metav1.DryRunAll}}`. The API server performs full validation without persisting changes.

**SSA path** (`serverSideApply=true`):
Uses `dynamic.Resource(gvr).Patch()` with `types.ApplyPatchType` and `Force: true`, matching `kubectl apply --server-side --force-conflicts`. Field manager is `"hydra"`.

**Client-side apply path** (`serverSideApply=false`):
Replicates `kubectl apply` behavior using the `last-applied-configuration` annotation and 3-way JSON merge patch:

1. Sets `kubectl.kubernetes.io/last-applied-configuration` annotation on the desired object
2. Attempts `Get` of the current object from the cluster
3. If not found: `Create` the object (with annotation)
4. If found: reads `last-applied-configuration` from the current object, computes a 3-way JSON merge patch via `jsonmergepatch.CreateThreeWayJSONMergePatch`, then applies with `Patch` using `MergePatchType`

**Note on merge strategy:** `kubectl apply` uses Strategic Merge Patch for native Kubernetes types (which merges arrays by key, e.g. container `name`) and falls back to JSON Merge Patch for CRDs. This implementation uses JSON Merge Patch for all types. The practical difference only matters when arrays are partially managed by multiple controllers. Since Hydra deploys complete manifests as the sole field owner (and sidecar injection modifies pods, not Deployment specs), JSON Merge Patch produces equivalent results.

Returns a kubectl-style output string (e.g. `configmap/my-config created\n`, `deployment.apps/my-deployment configured\n`).

**Caller changes:** The callers in `cluster_apply.go` are updated:

- `clusterDynamicClient()` is replaced by `k8s.NewClusterClient()` (returns `*ClusterClient`)
- `applyEntitiesBySSA()` receives a `*ClusterClient` parameter
- `standardApply()` and `bootstrapApply()` create a `ClusterClient` and pass it through
- The `ToYaml()` calls before `k8s.Apply` are removed; entities are passed directly

### Unit Tests (Apply)

1. SSA: single namespaced resource is created via Patch with ApplyPatchType
2. SSA: cluster-scoped resource (no namespace) is handled correctly
3. Client-side apply: new resource is created with `last-applied-configuration` annotation
4. Client-side apply: existing resource is updated via 3-way JSON merge patch
5. Client-side apply: unchanged resource produces "unchanged" output
6. DryRun: resources are not persisted when dryRun is true
7. Empty entities: no-op, returns empty output
8. Multiple entities: all are applied, output concatenated
9. Entity without unstructured data: returns error
10. GVR resolution failure: returns error

### Unit Tests (ShouldServerSideApply)

1. Entity with annotation `"ServerSideApply=true"` → `true`
2. Entity without annotation → `false`
3. Entity with annotation containing other options too → `true` (e.g. `"Prune=true,ServerSideApply=true"`)
4. Entity with annotation `"ServerSideApply=false"` → `false`
5. Entity without unstructured data → `false`

## Apply Helper Functions

### WaitForCRDsEstablished

```go
func WaitForCRDsEstablished(
    ctx context.Context,
    l log.Logger,
    dynamicClient dynamic.Interface,
    crdNames []string,
    timeout time.Duration,
) error
```

**Source file:** `core/k8s/crds.go`

Polls all CRDs in parallel until each has the `Established` condition set to `True`. The `timeout` parameter is configurable via `--crd-timeout` (default `60s`). Returns `ErrCrdEstablishTimeout` if any CRD does not become established within the timeout.

#### Unit Tests (WaitForCRDsEstablished)

1. All CRDs already Established → returns immediately
2. CRD becomes Established within timeout → success
3. CRD not Established within timeout → `ErrCrdEstablishTimeout`
4. Empty CRD list → no-op, returns nil
5. CRD does not exist (apply failed) → returns error

### DeleteWebhookConfigs

```go
func DeleteWebhookConfigs(
    ctx context.Context,
    l log.Logger,
    dynamicClient dynamic.Interface,
    webhookEntities entity.Entities,
    key types.EntityKeyUnstructured,
    dryRun types.DryRun,
) error
```text

**Source file:** `core/k8s/webhooks.go`

Deletes webhook configurations from the cluster. Used by call sites that delete webhook entities explicitly (for example `DeleteResources` during uninstall splits webhook entities and deletes them in its first internal phase). For each entity, determines if it is a `ValidatingWebhookConfiguration` or `MutatingWebhookConfiguration` and uses the appropriate GVR to delete via the Kubernetes dynamic client. Uses `errors.IsNotFound` to handle already-deleted webhooks gracefully.

During `hydra gitops apply`, orphaned webhooks are removed together with other orphans in the final `phaseDeleteOrphans` step (not via this helper exclusively).

#### Unit Tests (DeleteWebhookConfigs)

1. Delete existing `ValidatingWebhookConfiguration` → success
2. Delete non-existent webhook → no error (`IsNotFound` handled)
3. Delete both Validating and Mutating webhook configs → both deleted
4. Empty webhook entities → no-op
5. Dry-run mode → logged but not executed

### DisableWebhookConfigs

```go
func DisableWebhookConfigs(
    ctx context.Context,
    l log.Logger,
    dynamicClient dynamic.Interface,
    webhookEntities entity.Entities,
    key types.EntityKeyUnstructured,
    dryRun types.DryRun,
) error
```

**Source file:** `core/k8s/webhooks.go`

Disables webhook configurations by patching each webhook entry's `failurePolicy` to `"Ignore"`. This is less invasive than deleting the webhook configuration: the configuration remains in the cluster, and the later webhook apply phase restores the correct `failurePolicy` via SSA.

For each webhook entity:

1. Determine the GVR (ValidatingWebhookConfiguration or MutatingWebhookConfiguration)
2. GET the current webhook configuration from the cluster
3. Read `.webhooks[]` entries and their `failurePolicy` values
4. If all entries already have `failurePolicy: Ignore`, skip
5. Build a Strategic Merge Patch that sets `failurePolicy: "Ignore"` for each webhook entry (matched by `name` field, which is the merge key for the webhooks list)
6. Apply the patch

If the webhook configuration does not exist in the cluster (NotFound), it is skipped silently.

#### Unit Tests (DisableWebhookConfigs)

1. Existing ValidatingWebhookConfiguration → failurePolicy patched to Ignore
2. Existing MutatingWebhookConfiguration → failurePolicy patched to Ignore
3. Non-existent webhook → skipped silently
4. Already Ignore → no patch applied
5. Dry-run mode → no changes made
6. Multiple webhooks in one configuration → all patched

### IsWebhookProviderReady

```go
func IsWebhookProviderReady(
    ctx context.Context,
    dynamicClient dynamic.Interface,
    provider commands.WebhookProvider,
) (bool, error)
```text

**Source file:** `core/k8s/webhooks.go`

Checks whether a webhook provider's backing workload has at least one ready replica. Queries the cluster for the workload's current status via the dynamic client.

- Deployment/StatefulSet: checks `status.readyReplicas != 0`
- DaemonSet: checks `status.numberReady != 0`
- If the workload is not found in the cluster: returns false (not ready)

#### Unit Tests (IsWebhookProviderReady)

1. Deployment with readyReplicas > 0 → true
2. Deployment with readyReplicas == 0 → false
3. Deployment not found → false
4. StatefulSet with readyReplicas > 0 → true

### ZeroWorkloads

```go
func ZeroWorkloads(entities entity.Entities, key types.EntityKeyUnstructured, customWorkloads ...map[types.GVKString]types.HydraScaleGroup) (entity.Entities, error)
```

**Source file:** `core/commands/scale.go`

Pure function that modifies workload entities for scale-zero deployment. Returns a copy of the entities with workload specs adjusted. Non-workload entities are returned unchanged. For custom workloads, `ZeroWorkloads` sets each `replicaPath` to 0 (same as scale-down patching, but on the in-memory entity for the apply-at-scale-zero phase).

#### Unit Tests (ZeroWorkloads)

1. Deployment with replicas=3 → replicas set to 0
2. StatefulSet with replicas=0 → remains 0 (no-op)
3. DaemonSet with nodeSelector `{app: web}` → nodeSelector replaced with `{"hydra-gitops.org/hydra-disabled": "true"}`
4. DaemonSet without nodeSelector → nodeSelector set to `{"hydra-gitops.org/hydra-disabled": "true"}`
5. ConfigMap → unchanged
6. Entity without unstructured data → skipped

#### Unit Tests (ZeroWorkloads — Custom Workloads)

| Test                                                   | Verifies                                             |
| ------------------------------------------------------ | ---------------------------------------------------- |
| `TestZeroWorkloads_CustomWorkloadReplicasSetToZero`    | Kafka CR with spec.kafka.replicas=3 → set to 0       |
| `TestZeroWorkloads_CustomWorkloadMultipleReplicaPaths` | All replicaPaths set to 0                            |
| `TestZeroWorkloads_CustomWorkloadUnchangedOtherFields` | Non-replica fields in the custom CR remain unchanged |

Additional scenarios:

1. Non-workload entity (ConfigMap) → unchanged

### applyEntitiesBySSA

```go
func applyEntitiesBySSA(l log.Logger, entities entity.Entities, key types.EntityKeyUnstructured, dryRun types.DryRun) error
```text

**Source file:** `cli/action/cluster_apply.go`

Splits entities into SSA and regular groups using `ShouldServerSideApply`, serializes each group to YAML via `ToYaml`, and calls `k8s.Apply` twice — once for the regular group (without `--server-side`) and once for the SSA group (with `--server-side --force-conflicts`). Used by the scale-zero resource apply phase and the webhook apply phase in the automatically numbered apply plan.

### splitNamespaces

```go
func splitNamespaces(entities entity.Entities) (namespaces entity.Entities, rest entity.Entities, err error)
```

**Source file:** `cli/action/cluster_apply.go`

Splits the given entities into two groups: Namespace entities (GVK == `v1/Namespace`) and all remaining entities. Used by the namespace apply phase in both standard and bootstrap apply plans to deploy namespaces before other resources.

#### Unit Tests (splitNamespaces)

1. Mixed entities (Namespaces, CRDs, regular resources) — Namespaces correctly extracted, rest returned unchanged
2. No Namespace entities present — empty namespace list, all entities in rest
3. Only Namespace entities — all in namespace list, rest is empty

### SplitWebhooks

```go
func SplitWebhooks(entities entity.Entities) (webhooks entity.Entities, rest entity.Entities, err error)
```text

**Source file:** `core/commands/delete.go` (exported, shared function)

Splits the given entities into two groups: webhook configuration entities (`ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration`) and all remaining entities. Same pattern as `splitCRDs` and `splitNamespaces`. Used in both apply plans to defer webhook configurations to the dedicated webhook apply phase, and in `DeleteResources` to delete webhook configs before scale-down. Previously this was an unexported `splitWebhooks` function in `cli/action/cluster_apply.go`; it has been moved to `core/commands/` and exported as `SplitWebhooks` so that `DeleteResources` can also use it.

#### Unit Tests (SplitWebhooks)

1. Mixed entities (webhook configs, CRDs, regular resources) — webhook configs correctly extracted, rest returned unchanged
2. No webhook config entities — empty webhook list, all entities in rest
3. Only webhook config entities — all in webhook list, rest is empty
4. Both `ValidatingWebhookConfiguration` and `MutatingWebhookConfiguration` are extracted
