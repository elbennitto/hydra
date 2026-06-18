# Entity Architecture

## Overview

Entities represent Kubernetes resources throughout the Hydra system. An Entity is a typed key-value map that stores all metadata, labels, annotations, and the full unstructured resource content. Entities are created from rendered Helm manifests and form the basis for reference discovery, grouping, and dependency visualization.

**Source files:** `core/entity/entity.go`, `core/entity/entities.go`, `core/entity/entities_compare.go`, `core/entity/entities_group.go`, `core/entity/entities_select.go`, `core/entity/entities_sort.go`, `core/entity/entity_map.go`, `core/entity/order.go`, `core/entity/tools.go`

## Data Model

### Entity

```go
type Entity map[EntityKey]Value
```

An Entity is a map from typed keys to typed values. This design allows flexible extension without struct field changes.

### Entity Keys

Entity keys are typed constants that identify each stored property:

| Key                 | Type                             | Description                                                  |
| ------------------- | -------------------------------- | ------------------------------------------------------------ |
| `KeyId`             | `EntityKeyString`                | Fully qualified ID (e.g. `apps/v1/Deployment/default/nginx`) |
| `KeyGVK`            | `EntityKeyString`                | Group/Version/Kind (e.g. `apps/v1/Deployment`)               |
| `KeyGVR`            | `EntityKeyString`                | Group/Version/Resource                                       |
| `KeyGroup`          | `EntityKeyString`                | API group (e.g. `apps`, empty for core API)                  |
| `KeyVersion`        | `EntityKeyString`                | API version (e.g. `v1`)                                      |
| `KeyKind`           | `EntityKeyString`                | Resource kind (e.g. `Deployment`)                            |
| `KeyResource`       | `EntityKeyString`                | API resource (e.g. `deployments`)                            |
| `KeyName`           | `EntityKeyString`                | Resource name                                                |
| `KeyNamespace`      | `EntityKeyString`                | Namespace (empty for cluster-scoped)                         |
| `KeyAppNamespace`   | `EntityKeyString`                | Application namespace                                        |
| `KeyApiVersion`     | `EntityKeyString`                | Full API version (`group/version` or `version`)              |
| `KeyAppIds`         | `EntityKeySlice[AppId]`          | Hydra AppIds that produced this entity                       |
| `KeySelected`       | `EntityKeyBool`                  | Whether entity is marked for selection                       |
| `KeyNamespaced`     | `EntityKeyBool`                  | Whether resource is namespace-scoped                         |
| `KeyVerbs`          | `EntityKeySlice[KubernetesVerb]` | Supported Kubernetes verbs                                   |
| `KeyTemplatePath`   | `EntityKeyString`                | Helm template source file path                               |
| `KeyTemplateIndex`  | `EntityKeyInt`                   | 1-based position within template file                        |
| `KeyTemplateEntity` | `EntityKeyUnstructured`          | Full unstructured Kubernetes resource (from template)        |
| `KeyClusterEntity`  | `EntityKeyUnstructured`          | Live cluster version of the resource                         |
| `KeyEntity`         | `EntityKeyUnstructured`          | Copy of unstructured resource (used for CEL evaluation)      |
| `KeyBackupEntity`   | `EntityKeyUnstructured`          | Backup version of the resource                               |
| `KeyDryRunEntity`   | `EntityKeyUnstructured`          | Server-side dry-run result of the resource                   |
| `KeyLeftEntity`     | `EntityKeyUnstructured`          | Left-side entity for comparison                              |
| `KeyRightEntity`    | `EntityKeyUnstructured`          | Right-side entity for comparison                             |

### Value Types

```go
type Value interface {
    Value() any
    DeepCopy() Value
}

type ValueString struct { v string }
type ValueBool struct { v bool }
type ValueInt struct { v int }
type ValueSlice[T any] struct { v []T }
type ValueUnstructured struct { v unstructured.Unstructured }
```

Values are constructed via factory functions: `NewValueString()`, `NewValueBool()`, `NewValueInt()`, `NewValueSlice()`, `NewValueUnstructured()`. `ValueSlice` is generic — it is used for `[]AppId` (in `KeyAppIds`) and `[]KubernetesVerb` (in `KeyVerbs`).

### Entity ID Format

The `Id` type encodes group, version, kind, namespace, and name:

```text
group/version/kind/namespace/name    (with group)
version/kind/namespace/name          (core API, no group)

Examples:
  apps/v1/Deployment/default/nginx
  v1/ConfigMap/kube-system/coredns
  rbac.authorization.k8s.io/v1/ClusterRole//admin    (cluster-scoped: empty namespace)
```

For **namespaced** kinds, a `//` segment between kind and name means the namespace field is empty in the serialized ID (for example `v1/Service//frontend`). That is not a valid long-term state for rendered app entities: Helm manifests often omit `metadata.namespace` because the release namespace is implicit, so Hydra must copy the app namespace onto the entity and template `Unstructured` during render (`ApplyScopeInfoMap` / `ApplyScopeInfoMaps`, same as the `RenderCluster` path) before reference review or other ID-keyed logic runs. Cluster-scoped resources legitimately use an empty namespace segment in the ID.

**Parsing:** `Id.Parse()` splits the ID string into its components:

```go
type Id string

func (id Id) Group() Group
func (id Id) Version() Version
func (id Id) Kind() Kind
func (id Id) Namespace() Namespace
func (id Id) Name() Name
func (id Id) GVK() GVK
func (id Id) ApiVersion() ApiVersion
```

## Entity Creation

### From Rendered Helm Manifest

```go
func NewEntitiesFromYaml(l log.Logger, manifest YamlString, key EntityKeyUnstructured) (Entities, error)
```

The `key` parameter specifies under which unstructured key the parsed resources are stored (e.g. `KeyTemplateEntity` for rendered templates, `KeyClusterEntity` for live resources).

**Pipeline:**

```text
Rendered YAML manifest (with # Source: comments)
  │
  ▼
helm.SplitManifestMap()
  │  Splits by --- separator
  │  Extracts # Source: paths
  │  Returns map[path][]YamlString
  │
  ▼
For each (path, documents):
  For each (index, document):
    │
    ▼
    yaml.YamlToUnstructured()
    │  Parses YAML to Kubernetes Unstructured object
    │
    ▼
    Extract GVK, name, namespace from Unstructured
    │
    ▼
    Create Entity with keys:
    │  KeyGVK (group, version, kind via WithGVK)
    │  KeyName = resource name
    │  KeyNamespace = namespace (if present)
    │  KeyTemplatePath = path
    │  KeyTemplateIndex = index (1-based)
    │  <key> = unstructured (key is the provided EntityKeyUnstructured)
    │
    ▼
  Add to Entities collection
```

**Duplicate handling:** If multiple entities have the same ID, the **last** one wins. This matches Kubernetes behavior where later documents in a manifest override earlier ones.

### From Cluster (Live Resources)

Entities can also be created from live cluster resources via `k8s` operations. These entities have `KeyClusterEntity` set instead of (or in addition to) `KeyTemplateEntity`.

## Entities Collection

```go
type Entities struct {
    Items     []Entity              // Ordered list of all entities
    EntityMap EntityMap             // Map[Id]Entity for fast lookups
    IdSet     sets.Set[types.Id]   // Set of all entity IDs
    IdList    []types.Id           // Ordered list of entity IDs
}
```

### Key Methods

| Method                                  | Description                                                         |
| --------------------------------------- | ------------------------------------------------------------------- |
| `NewEntities(entities []Entity)`        | Creates collection, detects duplicates, returns `(Entities, error)` |
| `NewEntitiesFromYaml(l, manifest, key)` | Creates from rendered manifest                                      |
| `MergeEntities(items ...[]Entity)`      | Merges multiple entity slices into one collection                   |
| `Len()`                                 | Number of entities                                                  |
| `Add(other)`                            | Merges two collections, returns `(Entities, error)`                 |
| `Append(other)`                         | Appends another collection                                          |
| `Without(other)`                        | Returns entities not in other                                       |
| `Selected()`                            | Returns entities marked with KeySelected                            |
| `UnselectAll()`                         | Removes KeySelected from all entities                               |
| `Required(key)`                         | Filters to entities that have the given unstructured key            |
| `UidMap(key)`                           | Creates UID → Entity lookup map                                     |
| `RootOwnerUidMap(key)`                  | Builds root-owner → owned-UIDs map                                  |
| `ToYaml(key)`                           | Serializes entities to YAML string                                  |

## Entity Operations

### Comparison

```go
func (entities Entities) Compare(leftKey, rightKey types.EntityKeyUnstructured) (CompareResult, error)
```

Compares entities within a single collection by two different unstructured keys (e.g. `KeyTemplateEntity` vs. `KeyClusterEntity`):

1. Select entities containing each key
2. Split into `LeftOnly`, `RightOnly`, and `Both` (matched by ID)
3. For `Both`: merge keys from both sides into a single entity

```go
type CompareResult struct {
    All       Entities
    LeftOnly  Entities
    RightOnly Entities
    Both      Entities
}
```

### Selection

```go
func (entities Entities) Select(predicates ...func(Entity) (bool, error)) (Entities, Entities, error)
```

Filters entities using one or more predicates (OR logic — first match wins). Returns `(allWithMarks, matchedOnly, error)`. Matched entities in the first return value are marked with `KeySelected = true`.

**Built-in selectors:**

| Selector                         | Description                             |
| -------------------------------- | --------------------------------------- |
| `SelectByIds(ids ...Id)`         | Match by ID(s)                          |
| `SelectByIdSet(ids)`             | Match by ID set                         |
| `SelectByGvk(gvk)`               | Match by GroupVersionKind               |
| `SelectByGvkString(gvk)`         | Match by GVK string                     |
| `SelectByAppIds(appIds)`         | Match by Hydra AppId set                |
| `SelectByNamespaces(namespaces)` | Match by namespace set                  |
| `SelectByContainsEntityKey(key)` | Match entities that have a specific key |
| `SelectCrds()`                   | Match CustomResourceDefinitions         |
| `SelectNamespaces()`             | Match Namespace resources               |

### Grouping

```go
func GroupBy[T comparable](entities Entities, keyFunc func(Entity) (T, error)) (map[T]Entities, error)
```

Groups entities by an arbitrary key function. Returns a map from key to sub-collection.

**Built-in groupings:**

| Grouping                        | Key                      | Description                                |
| ------------------------------- | ------------------------ | ------------------------------------------ |
| `GroupByGVR()`                  | `types.GVR`              | Groups by API resource type                |
| `GroupByNamespace()`            | `types.Namespace`        | Groups by namespace                        |
| `GroupByLabel(key, label)`      | `types.LabelValue`       | Groups by label value                      |
| `GroupByLabels(key, labels)`    | Composite string         | Nested grouping by multiple labels         |
| `GroupByComponentInstance(key)` | app.kubernetes.io labels | Groups by Kubernetes app labels            |
| `GroupByOwnerId(key)`           | `types.Uid`              | Groups by root owner UID                   |
| `AllOwnerUids(key)`             | `types.Uid`              | Maps each UID to all transitive owner UIDs |

### Sorting

```go
func (entities Entities) Sort(orderProvider OrderProvider) (Entities, error)
```

Sorts entities using a composable ordering system. The sort is performed by grouping entities by key, sorting the keys, and recursing for sub-orders:

```go
type OrderProvider interface {
    Order() []Order
}

type Order interface {
    Direction() types.Direction
    Key(e Entity) (Key, error)
}
```

**Built-in orderings:**

| Order                                           | Description                                 |
| ----------------------------------------------- | ------------------------------------------- |
| `NewFieldOrder(direction, field)`               | Sort by entity field value                  |
| `NewReverseOrder(order)`                        | Reverse an existing order                   |
| `NewChainOrder(orders...)`                      | Multi-level sort (first order wins on tie)  |
| `NewFieldSetOrder(direction, field, values...)` | Sort by position in a predefined value list |
| `NewIdFieldOrder(direction)`                    | Sort by entity ID                           |
| `NewOrderFunc(direction, fn)`                   | Sort by custom function                     |

### Owner Reference Resolution

```go
func (entities Entities) RootOwnerUidMap(key types.EntityKeyUnstructured) map[types.Uid]sets.Set[types.Uid]
```

Builds a map from each root owner UID to all transitively owned entity UIDs. Used for finding all resources owned by a specific controller. The root owner itself is **not** included in its own set.

**Algorithm:**

1. Build UID-to-entity map via `UidMap(key)`
2. For each entity, recursively traverse ownership chain to find root owner
3. Collect all UIDs in the chain under the root owner
4. Return map: rootUID → Set{ownedUID1, ownedUID2, ...}

## Integration

Entities are the central data type used throughout Hydra:

```text
helm.Template() → YAML manifest
  │
  ▼
entity.NewEntitiesFromYaml(l, manifest, key) → Entities
  │
  ├── references.Refs(l, entities, key) → []Ref
  │     Discovers references between entities
  │
  ├── view.ToModel(l, entities) → DependenciesModel
  │     Computes groups and builds dependency graph
  │
  ├── commands.MarkAsSelected*(entities)
  │     Marks entities for deletion/uninstall
  │
  └── entity.Compare(template, cluster)
        Compares rendered vs. live entities
```

## Planned: unstructured key hygiene and CEL-backed refresh

The following helpers are specified for an upcoming change to `hydra gitops scale up|down` pod reconciliation. They belong in the entity layer (or thin wrappers in `core/commands` that delegate to `entity`) so list/merge behavior stays consistent with `Entities` invariants.

### Strip one unstructured key from all entities

**Behavior:**

1. For each entity in the collection, delete the given `EntityKeyUnstructured` (for example `KeyClusterEntity`) from the entity map if present.
2. After that, if an entity has **no** remaining unstructured keys (none of `KeyTemplateEntity`, `KeyClusterEntity`, `KeyEntity`, `KeyBackupEntity`, `KeyDryRunEntity`, `KeyLeftEntity`, `KeyRightEntity`, or any other unstructured key type in use), remove that entity from the ordered list and rebuild lookup structures (`EntityMap`, `IdSet`, `IdList`) like other mutating paths.
3. **Template-only entities must be retained:** an entity counts as template-only when it still carries template-origin unstructured content (concretely: it retains `KeyTemplateEntity` after the strip). Such entities are never dropped by this pass—even if the strip was meant to drop stale live views—because they still represent Helm-desired objects for downstream commands.

**Callers** use this to invalidate a whole “view” of the cluster (for example all live `Unstructured` under `KeyClusterEntity`) before re-listing from the API.

### `Entities` refresh (CEL + unstructured key)

**Signature shape (conceptual):** refresh takes a CEL predicate string, an unstructured key `k` to strip first, and the dependencies needed to list live resources (dynamic client, REST mapper, logger, etc.—exact parameters are an implementation detail).

**Ordered steps:**

1. Run the **strip-one-key** helper above for key `k` across the current `Entities` value.
2. List live API objects that match the CEL expression (same CEL resource-filter style as `ListCluster` and [`--include` / `--exclude` in cluster commands](../../../manual/cli/README.md#cel-resource-filters); not the full ref-parser CEL surface unless explicitly shared).
3. Convert listed objects to `Entity` values with unstructured content under `k` (typically `KeyClusterEntity`).
4. **Merge** the newly built entities into the existing collection using the same merge semantics as `MergeEntities` / `Add`: identical IDs collapse to one entity; fields from the new slice override or combine per existing rules.

This refresh is **not** a full re-render from Helm; it only refreshes the live slice selected by CEL and merges it into the entity list the command already holds.

### `refreshAllPods`

A small command-layer helper will call the refresh function with:

- a CEL expression that selects **`v1/Pod`** objects in scope for the current operation (namespace / cluster bounds follow the same rules as the parent `cluster scale` invocation), and
- the unstructured key used for live inventory in that flow (expected: `KeyClusterEntity`).

So all Pod rows in the merged entity set reflect the API state after workloads have been scaled.

### Unit tests (entity package and merge)

When implementing, add or extend tests in `core/entity` (and any new files) to cover:

- Stripping a key removes it from every entity; entities with no unstructured keys afterward are removed **except** template-only entities (still holding `KeyTemplateEntity`).
- After strip, `EntityMap` / `IdSet` / `IdList` stay consistent with `Items`.
- Refresh: strip + mock list + merge produces the expected ID set and per-entity keys (no duplicate IDs; merged entity contains new live unstructured).
- Edge case: strip `KeyClusterEntity` from an entity that has both template and cluster unstructured leaves the template side intact.

See also [Cluster scale flow — Pod reconciliation](commands/deletion-and-topology/topology-and-scaling/cluster-scale-flow.md#pod-reconciliation-after-scale-planned).
