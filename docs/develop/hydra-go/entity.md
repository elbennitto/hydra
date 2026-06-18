# Entity Architecture

Entities represent Kubernetes resources throughout the Hydra system. An Entity is a typed key-value map that stores all metadata, labels, annotations, and the full unstructured resource content. Entities are created from rendered Helm manifests and form the basis for reference discovery, grouping, and dependency visualization.

## Key Concepts

- **Entity** — `map[EntityKey]Value` with typed keys (KeyId, KeyGVK, KeyName, KeyNamespace, etc.) and typed values (string, bool, int, slice, unstructured)
- **Entity ID format** — `group/version/kind/namespace/name` (e.g. `apps/v1/Deployment/default/nginx`). For namespaced kinds, an empty namespace in the ID appears as a double slash (for example `v1/Service//nginx`); render paths must normalize omitted `metadata.namespace` to the app namespace before ref graphs and lookups rely on `Id()` (see `references.md` and commands rendering details). When the cluster render path rewrites `apiVersion` to the API server’s preferred version for a group/kind (`NormalizeApiVersions`), reference resolution applies the same preferred-version mapping to id endpoints in `RefDefinition`s so incoming/outgoing matching stays consistent (see `references.md`).
- **Entities collection** — Ordered list with fast ID-based lookup via `EntityMap` and `IdSet`
- **Creation** — From rendered Helm manifests via `NewEntitiesFromYaml()` or from live cluster resources
- **Comparison** — `Compare(leftKey, rightKey)` splits entities into LeftOnly, RightOnly, and Both
- **Selection** — Filter by predicates (ID, GVK, AppId, namespace, key presence) with OR logic
- **Grouping** — `GroupBy` with key functions (GVR, namespace, label, component, owner)
- **Sorting** — Composable ordering system with field, reverse, chain, and set orders
- **Owner resolution** — `RootOwnerUidMap` traces ownership chains for controller-managed resources
- **Planned: unstructured key stripping** — A generic collection helper will remove one `EntityKeyUnstructured` (for example `KeyClusterEntity`) from every entity, drop entities that afterward hold no unstructured keys (with an exception so **template-only** entities are never dropped), then consumers merge freshly listed objects back in (see [details/entity.md](details/entity.md#planned-unstructured-key-hygiene-and-cel-backed-refresh))
- **Planned: CEL-backed refresh** — A refresh-style API on `Entities` will re-list live objects matching a CEL predicate, strip a chosen unstructured key from the current set, and merge the new live slice back in; `hydra gitops scale` will use this (via a small `refreshAllPods` helper) to reconcile `Pod` state after scale operations

## Source Files

`core/entity/entity.go`, `core/entity/entities.go`, `core/entity/entities_compare.go`, `core/entity/entities_group.go`, `core/entity/entities_select.go`, `core/entity/entities_sort.go`, `core/entity/entity_map.go`, `core/entity/order.go`, `core/entity/tools.go`

→ **Full details:** [details/entity.md](details/entity.md)
