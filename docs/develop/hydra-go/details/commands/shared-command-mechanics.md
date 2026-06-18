# Commands: Shared Command Mechanics

This file collects cross-cutting command mechanics, shared data structures, wildcard resolution, and auxiliary command flows.

Back to [Commands detail index](../commands.md).

## Overview

The commands package (`core/commands/`) contains high-level orchestration functions for cluster operations. These functions are called by CLI action handlers and compose lower-level operations (entity processing, Helm rendering, Kubernetes API calls) into coherent workflows.

**Source files:** `core/commands/render.go`, `core/commands/cluster.go`, `core/commands/delete.go`, `core/commands/list_cluster.go`, `core/commands/mark_as_selected.go`, `core/commands/namespace.go`, `core/commands/topo_execute.go`, `core/commands/scale.go`, `core/commands/sync.go`, `core/commands/visit.go`, `core/commands/backup.go`, `core/commands/scope_info.go`, `core/commands/server_side_apply.go`, `core/commands/server_side_apply_annotation.go`, `core/commands/uninstall_finalizer.go`, `core/commands/webhook.go`

**See also:** [Cluster Diff Architecture](../diff.md) for the diff-specific data flow and diff modes.

## API Version Normalization

### Purpose

When Helm chart templates use a deprecated API version (e.g., `kafka.strimzi.io/v1beta2/Kafka`) but the Kubernetes cluster serves a newer preferred version (e.g., `kafka.strimzi.io/v1/Kafka`), entity IDs don't match during merge/compare operations. Entity IDs have the format `group/version/kind/namespace/name` — the version is part of the ID. Templates rendering `v1beta2` and the cluster listing `v1` (via `ServerPreferredResources()`) produce different IDs, causing valid resources to be incorrectly detected as orphans and deleted.

**Source files:** `core/commands/scope_info.go` (new function), `core/commands/render.go` (integration point), `core/types/scope_info.go` (new type)

### Precondition

For normalization to work, the CRD for the resource being normalized must be present in the rendered templates. `ScopeInfoMapFromCrds` adds all versions of a CRD to the scope info map, which allows entities with deprecated versions to pass through `ApplyScopeInfoMaps`. If the CRD is only in the cluster and not in templates, entities with the old version won't survive `ApplyScopeInfoMaps` (they would be skipped with `crdMode=ignore` or cause an error). This is fine because Hydra templates typically include CRDs.

### GroupKindKey and PreferredVersionMap

Defined in `core/types/scope_info.go`:

```go
type GroupKindKey string

func NewGroupKindKey(group Group, kind Kind) GroupKindKey {
    if group == "" {
        return GroupKindKey(string(kind))
    }
    return GroupKindKey(string(group) + "/" + string(kind))
}

func PreferredVersionMap(scopeInfoMap ScopeInfoMap) map[GroupKindKey]Version
```

`ScopeInfo` entries in `clusterScopeInfoMap` only contain `Namespaced` and `Resource` — they do NOT contain `Group`, `Version`, or `Kind`. Therefore `PreferredVersionMap` cannot use `ScopeInfo` as its value type. Instead, it parses each `GVKString` key in the `ScopeInfoMap` via `GVKString.Components()` (defined in `core/types/kubernetes.go`) to extract the group, version, and kind. It builds a `map[GroupKindKey]Version` where each key is `group/kind` and the value is the preferred `Version` extracted from the `GVKString` key. Since `clusterScopeInfoMap` comes from `ServerPreferredResources()`, the version in each key is always the preferred version.

For core resources with an empty group (e.g., `ConfigMap`, `Pod`), the `GroupKindKey` is just the kind itself.

### NormalizeApiVersions

```go
func NormalizeApiVersions(
    l log.Logger,
    entities entity.Entities,
    key types.EntityKeyUnstructured,
    clusterScopeInfoMap types.ScopeInfoMap,
) (entity.Entities, error)
```

**Source file:** `core/commands/scope_info.go`

**Algorithm:**

```text
1. Build PreferredVersionMap from clusterScopeInfoMap
   map[GroupKindKey]Version where GroupKindKey = "group/kind"
   (parses each GVKString key via .Components() to extract group, version, kind)
   │
   ▼
2. For each rendered entity:
   │  a. Get the entity's current group, version, and kind
   │  b. Look up group/kind in the PreferredVersionMap
   │  c. If not found → skip (resource not known to cluster, e.g. offline rendering)
   │  d. If found and version matches → skip (already correct)
   │  e. If found and version differs → normalize:
   │     - Log WARNING at most once per *Cluster instance (process lifetime) for the same
   │       (GVK, preferred version):
   │       "normalizing API version of {gvk} from {oldVersion} to {preferredVersion}"
   │       where {gvk} is `group/version/kind` (core resources: `version/kind`), i.e. the same
   │       string as `entity.GVKString()`. Namespace and name are not included. Deduplication keys
   │       are stored only on the in-memory `hydra.Cluster` object (not persisted). A new
   │       Hydra process or a fresh cluster instance logs again. If the cluster’s preferred
   │       version for that group/kind changes during the same run, a new warning may appear
   │       because the key includes the target preferred version.
   │     - Update entity metadata: WithVersion(preferredVersion)
   │     - Update unstructured data's apiVersion field via SetAPIVersion():
   │       The apiVersion string must be constructed correctly:
   │         - Empty group (core resources): just version (e.g., "v1")
   │         - Non-empty group: group + "/" + version (e.g., "kafka.strimzi.io/v1")
   │       After WithVersion(), the entity's ApiVersionString() method
   │       (core/entity/tools.go) returns the correct format
   │
   ▼
3. Return entity.NewEntities(updated) — rebuilds EntityMap and deduplicates by ID
```

**Duplicate IDs after normalization:** If templates contain both a deprecated and a preferred version of the same resource (e.g., `kafka.strimzi.io/v1beta2/Kafka/ns/my-kafka` AND `kafka.strimzi.io/v1/Kafka/ns/my-kafka`), normalization produces two entities with the same ID. This is a non-issue because `NewEntities()` deduplicates by ID (last one wins), and both versions normalize to the same preferred version, producing identical entities. Warnings and deduplication use **GVK** only (`GVKString()`), so all resources of the same group/version/kind share one deduplication bucket per target preferred version; the deprecated `version` in the GVK string still differs between v1beta2 and v1 templates, so each source API version can log once per cluster instance until recorded in memory.

### Integration in RenderCluster

`NormalizeApiVersions` is called in `RenderCluster()` (in `core/commands/render.go`) right after `ApplyScopeInfoMaps()`, before the return:

```text
clusterScopeInfoMap, err := ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity)
...
renderedEntities, err = ApplyScopeInfoMaps(crdMode, renderedEntities, types.KeyTemplateEntity, scopeInfoMaps..., requiredGVKs...)
...
// NEW: Normalize API versions to cluster's preferred versions
renderedEntities, err = NormalizeApiVersions(l, renderedEntities, types.KeyTemplateEntity, clusterScopeInfoMap)
```

**Offline mode:** When `KubernetesConnectionAllowed == No`, `clusterScopeInfoMap` is empty (already handled by existing code in `ScopeInfoMapFromCluster`), so `NormalizeApiVersions` finds no entries in the `PreferredVersionMap` and skips all entities — no special handling needed.

### Unit Tests (PreferredVersionMap)

Test file: `core/types/scope_info_test.go`

1. **Single entry** — correct `group/kind` key built, version extracted from `GVKString`
2. **Multiple entries same group different kinds** — separate entries
3. **Empty group (core resources)** — version extracted correctly, `GroupKindKey` is just kind (e.g., `"ConfigMap"`)
4. **Empty map** — returns empty map
5. **GVKString key with empty group** — `GVKString.Components()` returns empty group, version extracted correctly

### Unit Tests (NormalizeApiVersions)

Test file: `core/commands/scope_info_test.go`

1. **Entity with matching version** — version unchanged, no warning
2. **Entity with deprecated version** — version updated to preferred, warning logged with old entity ID
3. **Entity not in cluster scope** — version unchanged (offline rendering)
4. **Entity without unstructured data** — version metadata updated, no panic
5. **Multiple entities mixed** — only mismatched ones get updated
6. **Core resources (empty group)** — handles `v1/ConfigMap` correctly (`GroupKindKey = "ConfigMap"`), `SetAPIVersion` receives `"v1"` (not `"/v1"`)
7. **Duplicate IDs after normalization** — both entities normalized, `NewEntities` deduplicates (last wins)

## Wildcard App ID Matching

All commands that accept app IDs support glob-style wildcard matching. Patterns may contain `*` and `**` wildcards anywhere in the string.

### Wildcard Syntax

`*` matches zero or more characters **except** `.` (dot). It stays within a single segment of the dot-separated app ID.

`**` matches zero or more characters **including** `.` (dot). It crosses segment boundaries.

A pattern without any `*` is treated as an exact name (no glob matching, not validated against the app list).

| Pattern                            | Matches                                                         | Does NOT match                                          |
| ---------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------- |
| `*.*.*`                            | All child apps (3-segment IDs), e.g. `prod.cluster-infra.nginx` | `prod.cluster-infra` (2 segments)                       |
| `*.*`                              | All root apps (2-segment IDs), e.g. `prod.cluster-infra`        | `prod.cluster-infra.nginx` (3 segments)                 |
| `prod.*`                           | Root apps in cluster `prod`, e.g. `prod.demo`                    | `prod.demo.app1` (3 segments), `dev.demo` (wrong cluster) |
| `prod.*.*`                         | All child apps in cluster `prod`                                | `prod.demo` (2 segments)                                 |
| `prod.**`                          | All apps in cluster `prod` (root and child)                     | `dev.demo` (wrong cluster)                               |
| `prod.cluster-infra*`              | `prod.cluster-infra` (the `*` matches zero non-dot chars)       | `prod.cluster-infra.nginx` (`*` cannot cross `.`)       |
| `prod.cluster-infra.*`             | All child apps of `prod.cluster-infra`                          | `prod.cluster-infra` itself                             |
| `**`                               | All applications                                                | —                                                       |
| `in-cluster.cluster-infra.kyverno` | Exact match (no wildcard)                                       | —                                                       |

### --exclude-app Flag

All multi-app commands (`cluster apply`, `cluster diff`, `cluster template`, `cluster uninstall`, `cluster scale`, `cluster review`, `cluster status`, `hydra argocd status`, `hydra argocd sync`, `hydra local template`, `hydra local source`, and `hydra local review`) support the `--exclude-app` flag. It accepts app ID patterns with the same glob syntax and can be repeated.

Exclude patterns are resolved against the same app list as include patterns. The resolved exclude set is subtracted from the resolved include set. If an exclude pattern matches zero apps, a warning is logged (not an error).

**Exception — `hydra local review` and `hydra gitops review`:** For these commands, `--exclude-app` must narrow only which applications contribute **source** template entities (and therefore which apps can appear in finding `sources`). It must **not** shrink the **target** entity set: cluster review still loads **all** live cluster entities for target lookup; local review still includes templates from **every** effectively enabled app on the affected cluster in the target index. Implementation may therefore need a distinct code path from other multi-app commands that use a single subtracted `appIds` set for all phases.

```text
hydra gitops uninstall *.*.* --exclude-app prod.cluster-infra.ingress-nginx
hydra gitops apply prod.** --exclude-app prod.cluster-infra.cert-manager --exclude-app prod.cluster-infra.dex
hydra argocd sync prevent prod.** --exclude-app prod.cluster-infra.argocd
```

### --include / --exclude (CEL) on `hydra local template` and `hydra gitops template`

`hydra local template` and `hydra gitops template` support the same repeatable `--include` / `--exclude` CEL resource filters as `hydra local find` and `hydra gitops dump`. `hydra local template` builds template-scoped entities per app (`commands.TemplateRenderedEntities`, matching the `RenderClusterSelectedApps` pipeline), applies the combined predicate, and prints via `Entities.ToYaml` (multi-doc YAML without Helm `# Source:` headers). For `templatePatches`, it also merges the **full-cluster scope-catalog** render into Hydra ConfigMap rule collection so shared carriers (for example under Argo CD) apply. `hydra gitops template` applies the same predicate model **after** its cluster-aware pipeline (scope maps from live discovery + full-cluster CRD catalog, preferred `apiVersion` normalization, then `templatePatches` merged using the full-cluster catalog plus the selected-app render).

`hydra local source` does not render manifests and therefore does not accept CEL `--include` / `--exclude`. It reads template files from disk and supports repeatable `--include-path` values instead (OR semantics): strict prefix from the chart root at a path boundary, plus an alternate match when the value contains `/` and appears as a full segment sequence inside Helm’s template path (for umbrella charts that prefix names with the parent chart).

### Warning for Low Wildcard Match Count

When a wildcard pattern (one containing `*`) resolves to exactly **1** application, a warning is logged. This helps catch typos or overly specific patterns where a wildcard was likely unintended. Zero matches remain a hard error.

### Unified CLI Command Structure

Hydra's multi-app commands use direct app ID arguments instead of `app/root-app/cluster` selector subcommands:

```text
hydra gitops apply <appId> [appId...]
hydra gitops diff <appId> [appId...]
hydra gitops template <appId> [appId...]
hydra gitops uninstall <appId> [appId...]
hydra gitops scale up <appId> [appId...]
hydra gitops scale down <appId> [appId...]
hydra gitops status <appId> [appId...]
hydra gitops review app <appId> [appId...]
hydra gitops review cluster <cluster>
hydra argocd status [appId...]
hydra argocd sync auto <appId> [appId...]
hydra argocd sync manual <appId> [appId...]
hydra argocd sync prevent <appId> [appId...]
hydra local review <appId> [appId...]
hydra local template <appId> [appId...]
hydra local source <appId> [appId...]
hydra local values <appId>                        (single app only)
hydra local config <appId>                        (single app only)
```

All multi-app commands support the `*` wildcard syntax. Commands operating on a single cluster (`cluster apply`, `cluster diff`, `cluster template`, `cluster uninstall`, `cluster scale`, `cluster status`, and `cluster review app`) enforce a single-cluster constraint: all resolved app IDs must belong to the same cluster. `hydra gitops review cluster <name>` selects apps by repository cluster directory instead of patterns. `hydra local template`, `hydra local source`, and `hydra local review` are offline commands and therefore do not require cluster connectivity. `hydra gitops template` requires Kubernetes API connectivity for discovery (preferred API versions and live scope) while remaining read-only for resources. `hydra argocd status` and `hydra argocd sync` talk to ArgoCD on the selected cluster.

### Resolution Strategies

There are two resolution strategies depending on the data source:

1. **Config-based resolution** (`ResolveAppIdsFromConfig`): Used by `cluster apply`, `cluster diff`, `cluster template`, `cluster uninstall`, `cluster scale`, `cluster status`, `cluster review app`, `hydra local template`, `hydra local source`, and `hydra local review`. Enumerates clusters and apps from the filesystem. `cluster review cluster` uses `ResolveAppIdsInClusterWithExcludes` after resolving the cluster directory.
2. **Live ArgoCD resolution** (`ResolveAppNames`): Used by `hydra argocd status` and `hydra argocd sync`. Queries the Kubernetes API for ArgoCD Application resources.

### ResolveAppIdsFromConfig

```go
func ResolveAppIdsFromConfig(
    l log.Logger,
    hydraContext types.HydraContext,
    config types.Config,
    patterns []types.AppIdPattern,
    excludePatterns []types.AppIdPattern,
    networkMode types.HelmNetworkMode,
) (sets.Set[types.AppId], error)
```

**Source file:** `core/commands/resolve.go`

Resolves wildcard patterns against config-based app IDs from the filesystem. Supports both include and exclude patterns.

- Without `*` in any pattern: directly converts include patterns to `types.AppId` (validated via `types.NewAppId`), returns set. Exclude patterns are resolved literally and subtracted.
- With `*` in any pattern: creates offline config (`KubernetesConnectionAllowedNo`), resolves context via `hydra.ResolvePath`, enumerates clusters via `context.GetClusters()`, collects all app IDs via `cluster.AppIds(networkMode)`, then delegates to `ResolvePatterns` for both include and exclude patterns
- After resolving includes, resolves excludes against the same app list and subtracts them from the include set
- No single-cluster validation — that is the caller's responsibility (via `commands.ClusterForAppIds`)
- Logs warnings returned by `ResolvePatterns` (e.g. for single-match wildcards)

### ResolveAppNames

```go
func ResolveAppNames(h hydra.Hydra, patterns []string) ([]string, error)
```

**Source file:** `core/commands/sync.go`

Top-level resolution function that expands app-selection patterns to concrete ArgoCD application names. It uses the same wildcard semantics documented for the CLI generally: `*` matches within one dot-separated segment, `**` may cross dots, and exact names stay exact names.

- If the input contains no glob pattern at all: returns the given names as-is, without listing ArgoCD Applications first
- If at least one pattern contains `*`: lists ArgoCD Applications in namespace `argocd`, then resolves every include pattern through `ResolvePatterns` and `MatchAppIdGlob`
- If a wildcard pattern matches zero applications: returns an error mentioning that pattern
- Deduplicates results when multiple patterns match the same app
- Logs: `INFO "resolved '{pattern}' to {count} application(s)"` for wildcard patterns

**Optimization:** If no patterns contain `*`, the Kubernetes API call is skipped entirely and patterns are returned as-is. The application list is fetched at most once per `ResolveAppNames` call.

**Empty input:** `ResolveAppNames(..., []string{})` returns an empty slice without listing Applications. The caller defines what that means. For `hydra argocd status`, an empty resolved filter means "show all visible Applications". Mutating `hydra argocd sync` subcommands require app IDs at the CLI layer and therefore do not use an implicit all-apps mode.

Internally, `ResolveAppNames` calls the Kubernetes API to get the full list of application names, then delegates to the pure function `ResolvePatterns` for the actual matching logic.

### MatchAppIdGlob (Pure Function)

```go
func MatchAppIdGlob(pattern, name string) bool
```

**Source file:** `core/commands/sync.go`

Converts a glob pattern to a regular expression and tests whether `name` matches.

**Conversion rules** (applied left-to-right, `**` is consumed before `*`):

| Glob token | Regex equivalent | Meaning                                     |
| ---------- | ---------------- | ------------------------------------------- |
| `**`       | `.*`             | Match zero or more characters including `.` |
| `*`        | `[^.]*`          | Match zero or more characters excluding `.` |
| `.`        | `\.`             | Literal dot                                 |
| other char | escaped literal  | Literal character                           |

The resulting regex is anchored with `^` and `$`. The compiled regex is not cached (patterns are short and matched against small lists).

### ResolvePatterns (Pure Function)

```go
func ResolvePatterns(patterns []string, allAppNames []string) (resolved []string, warnings []string, err error)
```

**Source file:** `core/commands/sync.go`

Pure function extracted for testability. Given a list of patterns and a list of all known app names, returns the resolved list of app names plus any warnings.

**Algorithm:**

```text
For each pattern in patterns:
  │
  ├── Does NOT contain '*':
  │   Add pattern to result set as-is (exact name)
  │
  └── Contains '*':
      Filter allAppNames where MatchAppIdGlob(pattern, name) == true
      If zero matches → return error: "pattern '{pattern}' matched no applications"
      If exactly one match → append warning: "pattern '{pattern}' matched only 1 application: '{name}'"
      Add all matches to result set
  │
  ▼
Deduplicate result set
Sort alphabetically
Return sorted slice + collected warnings
```

### ArgoCD Status And Sync Split

**Primary source areas:** `cli/cmd/`, `cli/action/`, and `core/commands/sync.go`

The ArgoCD-facing status and sync operations live under a dedicated top-level command:

```text
hydra argocd status [appId...] [--exclude-app ...]
hydra argocd sync auto <appId...> [--exclude-app ...]
hydra argocd sync manual <appId...> [--exclude-app ...]
hydra argocd sync prevent <appId...> [--exclude-app ...]
```

The split intentionally creates two status surfaces with different semantics:

- `hydra argocd status` is read-only and shows the real sync state currently reported by ArgoCD.
- `hydra gitops status <appId...> [--exclude-app ...]` stays in the cluster command family and computes a per-app in-sync result from rendered desired state versus tracked live cluster resources.

### Integration In `hydra argocd sync`

The sync mutation flow remains ArgoCD-backed:

```text
1. cluster = ResolvePathWithCluster(...)
2. resolvedAppIds = ResolveAppNames(cluster, includePatterns)
3. excludedAppIds = ResolveAppNames(cluster, excludePatterns)
4. targetAppIds = resolvedAppIds - excludedAppIds
5. For each appId in targetAppIds:
     SyncSet(cluster, appId, kind, manualSync, dryRun)
```

### Integration In `hydra argocd status`

The ArgoCD status flow stays read-only and uses live ArgoCD data:

```text
1. cluster = ResolvePathWithCluster(...)
2. If includePatterns is empty:
     visibleAppIds = all visible Applications minus ResolveAppNames(cluster, excludePatterns)
   Else:
     resolvedAppIds = ResolveAppNames(cluster, includePatterns)
     excludedAppIds = ResolveAppNames(cluster, excludePatterns)
     visibleAppIds = resolvedAppIds - excludedAppIds
3. SyncStatus(cluster, color, visibleAppIds)
```

### Integration In `hydra gitops status`

The cluster-based status flow is config-backed and render-backed:

```text
1. Resolve app IDs from config via include patterns + --exclude-app
2. Enforce single-cluster selection
3. Render the selected apps
4. Collect tracked live resources for those apps
5. Compute and print an in-sync / out-of-sync result per app
```

### Unit Tests (MatchAppIdGlob)

Test file: `core/commands/sync_test.go`

1. **`*` does not match dot** — `MatchAppIdGlob("prod.*", "prod.demo.app1")` → `false`
2. **`*` matches within segment** — `MatchAppIdGlob("prod.*", "prod.demo")` → `true`
3. **`**`matches across dots** —`MatchAppIdGlob("prod.\*\*", "prod.demo.app1")`→`true`
4. **`*.*.*` matches 3-segment** — `MatchAppIdGlob("*.*.*", "prod.infra.nginx")` → `true`
5. **`*.*.*` does not match 2-segment** — `MatchAppIdGlob("*.*.*", "prod.infra")` → `false`
6. **Trailing `*` without dot** — `MatchAppIdGlob("prod.cluster-infra*", "prod.cluster-infra")` → `true`
7. **Trailing `*` without dot does not cross** — `MatchAppIdGlob("prod.cluster-infra*", "prod.cluster-infra.nginx")` → `false`
8. **`**`alone matches everything** —`MatchAppIdGlob("\*\*", "prod.infra.nginx")`→`true`
9. **Exact match without wildcard** — `MatchAppIdGlob("prod.infra", "prod.infra")` → `true`
10. **Exact match mismatch** — `MatchAppIdGlob("prod.infra", "prod.demo")` → `false`
11. **Mid-segment wildcard** — `MatchAppIdGlob("prod.cluster-*.*", "prod.cluster-infra.nginx")` → `true`

### Unit Tests (ResolvePatterns)

Test file: `core/commands/sync_test.go`

The `ResolvePatterns` pure function and `MatchAppIdGlob` are the primary unit test targets. `ResolveAppNames` is a thin wrapper that calls the Kubernetes API and delegates to `ResolvePatterns`, so mocking is not required for the core logic.

1. **Pattern without `*`** — returned as-is, not validated against `allAppNames`
2. **Pattern `prod.*`** with apps `[prod.a, prod.b, prod.a.child]` — returns `[prod.a, prod.b]` (no child apps, `*` does not cross dot)
3. **Pattern `prod.**`** with apps `[prod.a, prod.a.child, test.c]`— returns`[prod.a, prod.a.child]` (`\*\*` crosses dots)
4. **Pattern `foo*`** with no matching apps — returns error mentioning `foo*`
5. **Multiple patterns `[prod.a, prod.*]`** with apps `[prod.a, prod.b]` — returns `[prod.a, prod.b]` (deduplicated, sorted)
6. **Empty patterns** — returns empty slice
7. **Pattern `**`\*\* (double star) — matches all apps
8. **Multiple patterns, one fails** — `[prod.*, foo*]` with apps `[prod.a]` — returns error for `foo*`
9. **Pattern without `*` containing special chars** — pattern `prod*bar` treated as exact name (not a wildcard since no standalone `*`), returned as-is. Note: `prod*bar` does contain `*` so it IS treated as a glob; `MatchAppIdGlob("prod*bar", "prodbar")` → true, `MatchAppIdGlob("prod*bar", "prod.bar")` → false
10. **Wildcard `**` with empty app list\*\* — returns error (zero matches)
11. **Pattern `*.*.*`** with apps `[prod.a, prod.a.child1, prod.a.child2]` — returns `[prod.a.child1, prod.a.child2]`
12. **Warning for single match** — pattern `prod.infra.*` with apps where only 1 child matches — returns result + warning string

### Unit Tests (ArgoCD Status And Sync Split)

Test files: `core/commands/sync_test.go`, `cli/action/...`, and `cli/cmd/...`

1. **`hydra argocd status` is read-only** — reads ArgoCD application state without mutating AppProject sync
2. **ArgoCD status reports actual sync state** — output reflects the live ArgoCD sync status for each selected application
3. **ArgoCD status without app IDs** — an empty selector shows all visible Applications, then applies any `--exclude-app` subtraction
4. **ArgoCD status with `--exclude-app`** — excluded apps are removed from the visible result after wildcard resolution
5. **`hydra argocd sync auto|manual|prevent` with `--exclude-app`** — only the post-subtraction target set is mutated
6. **Top-level command split** — help text, synopsis, and examples document the `hydra argocd ...` surface consistently
7. **`hydra gitops status` selection** — resolves include patterns via config, subtracts `--exclude-app`, and enforces single-cluster scope
8. **`hydra gitops status` result semantics** — reports per-app in-sync versus out-of-sync based on rendered desired resources and tracked live cluster resources

### Unit Tests (ResolveAppIdsFromConfig)

Test file: `core/commands/resolve_test.go`

The tests exercise the non-wildcard path (direct `types.NewAppId` conversion), the wildcard path (using `ResolvePatterns` with mock app lists), and the exclude path:

1. **Exact app ID without wildcard** — directly converted to `types.AppId`
2. **Multiple exact app IDs** — all converted to set
3. **Invalid app ID format without wildcard** — returns error from `types.NewAppId`
4. **Empty patterns** — returns empty set
5. **`prod.*` wildcard** — matches root apps in cluster `prod` (not child apps)
6. **`prod.demo.*` wildcard** — matches all child apps of root app `demo`
7. **Wildcard with no match** — returns error mentioning the pattern
8. **Deduplication with overlapping patterns** — `prod.*` + `prod.demo.*` produce deduplicated set

## Data Flow (View / Dependency Graph)

```text
hydra local export cluster --context ./gitops production
  │
  ▼
action.ClusterViewCluster()
  │
  ├── RenderCluster(cluster, config, nil)
  │     Render all apps → entities
  │
  ├── view.ToModel(l, entities)
  │     Discover references
  │     Compute groups
  │     Build DependenciesModel
  │
  └── view.RenderDependencies(l, writer, entities)
        Compute model and serialize to .hydra.yaml
        → consumed by Hydra UI
```

## Error Types

| Error ID                 | Description                                                                     | Message                                                                                                                                                   |
| ------------------------ | ------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ErrScaleDownTimeout`    | Workload pods did not terminate within the configured timeout during scale-down | _"aborted: pods did not terminate within {timeout}. To retry, run the same command again. To re-scale workloads, run: hydra gitops scale \<params\> up"_ |
| `ErrCrdEstablishTimeout` | CRDs did not become Established within the configured timeout                   | _"aborted: CRDs did not become Established within {timeout}. Re-running the command is safe."_                                                            |
| `ErrScaleUpTimeout`      | Workload did not become ready within the configured timeout during scale-up     | _"aborted: workload {name} did not become ready within {timeout}. To retry, run: hydra gitops scale \<params\> up"_                                      |

Workloads with `replicas: 0` in the rendered template are included in scale-up but patched to 0 (no-op). The readiness check passes immediately since `0 == 0`.

## Types

### ScaleDirection

```go
type ScaleDirection string

const (
    ScaleUp   ScaleDirection = "up"
    ScaleDown ScaleDirection = "down"
)
```

### ScaleTarget

Replaces the old `ScaleDownTarget` (renamed, exported, extended with DaemonSet and custom workload support):

```go
type ScaleTarget struct {
    Id               types.Id
    Name             types.Name
    Ns               types.Namespace
    GVR              types.GVR
    GVK              types.GVKString
    Replicas         int64              // original replicas from rendered template (0 for DaemonSets)
    IsDaemonSet      bool
    NodeSelector     map[string]string  // original nodeSelector from rendered template (DaemonSets only)
    IsCustomWorkload bool               // true for custom CRD-based scale targets from global.hydra.scale
    ReplicaPaths     []string           // dot-separated paths to replica fields (custom workloads only)
    OriginalReplicas map[string]int64   // original values for each ReplicaPath from rendered template
}
```

## Test Commands

### hydra local test refs

```text
hydra local test refs <appId...> [--hydra-context PATH] [--update]
```

Tests app-defined ref-parsers from `global.hydra.refs` in Helm chart values against golden files stored in the chart's `test/refs/` directory.

**Source files:** `cli/cmd/test.go`, `cli/cmd/test_refs.go`, `cli/action/test_refs.go`

### Flags

| Flag              | Description                                            |
| ----------------- | ------------------------------------------------------ |
| `--hydra-context` | Path to the Hydra context (or `HYDRA_CONTEXT` env var) |
| `--update`        | Regenerate `.expected.yaml` files instead of comparing |

### Data Flow

```text
hydra local test refs <appId...>
  |
  v
For each appId:
  1. Load Hydra context, resolve app
  2. Read app values -> extract global.hydra.refs -> extraParsers
  3. Determine chart path from app source config
  4. Scan chart/test/refs/ for .given.yaml files
  5. For each test file:
     a. Parse entities from .given.yaml
     b. Run refDefinitions(entities, key, extraParsers)
     c. Run Refs(entities, key, extraParsers)
     d. If --update: write .expected.yaml
     e. Else: compare with .expected.yaml, report diff
```

### Test File Format

```text
<chart>/test/refs/
  <testname>.given.yaml      # input: multi-doc YAML with Kubernetes entities
  <testname>.expected.yaml   # output: expected refDefinitions + refs
```

The `.expected.yaml` format is identical to the built-in test format:

```yaml
refDefinitions:
  - owner: <gvk/ns/name>
    type: direct
    direction: outgoing
    endpoint:
      type: provider
      value: <provider-name>
refs:
  - refType: direct
    endpointType: provider
    from: <gvk/ns/name>
    to: <gvk/ns/name>
```

### CLI Structure

The `test` command family lives under `hydra local` and keeps its `refs` subcommand:

```text
hydra
  └── local
      └── test
          └── refs <appId...> [--hydra-context PATH] [--update]
```

Registered in `RootCommandParams` and `newRootCommand()`:

```go
type RootCommandParams struct {
    // ...existing fields...
    Test TestCommandParams
}

// In newRootCommand:
rootCmd.AddCommand(NewTestCommand(params.Test))
```

## Review Commands

### hydra local review

```text
hydra local review <appId...> [--hydra-context PATH] [--exclude-app PATTERN...]
```

Reviews reference integrity for the selected apps entirely locally. It renders the **selected** apps for **sources** (like `hydra local template` for that subset), additionally renders **all** effectively enabled apps on the same cluster for the **target** index, never talks to Kubernetes, and reports `missing target resource` only when a referenced target is absent from the target set **and** not accounted for by a ref with **`"origin:generated": job`** or **`"origin:generated": controller`** in that set (for example `SopsSecret` secret templates).

**Planned source areas:** top-level review command wiring in `cli/cmd/`, local review action wiring in `cli/action/`, and shared review orchestration in `core/commands/`

### Local Data Flow

```text
hydra local review <appId...>
  |
  v
1. Resolve app IDs from config via include patterns + --exclude-app (source app set)
2. Derive the single cluster name from that app set (same constraint as today)
3. Render template entities for the source app set (selected apps only)
4. Render template entities for all effectively enabled apps on that cluster (target app set;
   must not be reduced by --exclude-app)
5. Build resource-level refs from the source entity set only
6. Preserve explicit Secret/ConfigMap key selections as repeated relation attributes
   such as {type: "key", value: "SPRING_PROFILE"}
7. Resolve targets against the full target entity set (including indirect Secret/ConfigMap
   via declared materializations such as SopsSecret when modeled in review)
8. Restrict review findings to source resources from the source app set
9. Report `missing target resource` when a target object is absent and no in-set ref with
   `"origin:generated": job` / `"origin:generated": controller` defines it
10. Report missing referenced Secret/ConfigMap keys from explicit key attributes
11. Group repeated findings when target + message are identical across sources
12. Return a non-zero exit code when any finding exists
```

### hydra gitops review

```text
hydra gitops review app <appId...> [--hydra-context PATH] [--exclude-app PATTERN...]
hydra gitops review cluster <cluster> [--hydra-context PATH] [--exclude-app PATTERN...]
```

`review app` resolves the source app set from include patterns and `--exclude-app` (same as historical `hydra gitops review`). `review cluster` takes a repository **cluster name** (validated with `types.NewClusterName`: no `.`), loads `Cluster.AppIds` for that cluster, then applies `--exclude-app`. Both modes render only the resolved app set locally for sources and resolve targets from the live cluster.

`AppendRefOwnershipReviewFindings` receives `reportUnassignedClusterOnlyResources`: **false** for `review app` (suppresses `ref ownership: cluster-only resource has no Hydra app assignment`), **true** for `review cluster`. When reporting unassigned cluster-only resources, ids matching `IsKubernetesStandardRefOwnershipExempt` (upstream bootstrap catalog from `kubernetes_builtin_catalog.go` plus per-namespace `ServiceAccount/default` and `ConfigMap/kube-root-ca.crt`) are skipped. Objects whose `metadata.ownerReferences` resolve by UID to another entity in the live inventory are also skipped so only ownership-chain roots are listed.

For **`hydra gitops review`** only (not `hydra local review`), the merged enabled-app template render and each **standalone per-app** render passed into ref ownership are run through **`NormalizeApiVersions`** with the same discovery scope as filtered sources (`ScopeInfoMapFromCluster`), so `templateResourceIDToApp` keys use preferred API versions that match **`ListClusterAll`** identities.

**Planned source areas:** cluster review command wiring in `cli/cmd/`, cluster review action wiring in `cli/action/`, and shared review orchestration in `core/commands/`

### Cluster Data Flow

```text
hydra gitops review app <appId...>   |   hydra gitops review cluster <cluster>
  |                                              |
  v                                              v
1. Resolve app IDs (patterns + --exclude-app   |   all apps in cluster dir + --exclude-app)
   or cluster name -> app ids)                  |
2. Open the target Hydra cluster context         |
3. Render the source app set locally            |
4. List all live cluster entities for targets   |
5. Build refs, resolve targets, ref ownership   |
6. Unassigned cluster-only ownership findings   |   emitted only here (-> report flag true)
```

### Review-Specific Modeling

- Endpoints stay resource-oriented (`id(...)` still points to the target resource).
- Explicit key selections are attached to the edge as repeated structured attributes rather than tags.
- A single relation may therefore carry multiple key attributes:

```yaml
from: apps/v1/Deployment/demo/api
to: v1/ConfigMap/demo/api-config
labels: [env]
attributes:
  - key: SPRING_PROFILE
  - key: LOG_LEVEL
```

- `envFrom` does not produce `key` attributes and remains an existence-only check.

## Source Files Summary

| File                                            | Purpose                                                                                                                                                                                                                |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `core/commands/topo_execute.go`                 | `TopologicalExecute`, `BuildDependencyGraph`, `PlanTopologicalOrder`, `ReverseRefs` — dynamic DAG executor and plan computation                                                                                        |
| `core/commands/order.go`                        | **Removed** — `HydraApplyOrderProvider` and `HydraUninstallOrderProvider` no longer exist                                                                                                                              |
| `core/commands/delete.go`                       | `DeleteResources` (3-phase: webhook deletion, scale-down, foreground delete) + `collectScaleDownTargets` (unexported, with ownerReference filtering) + `SplitWebhooks` (exported, shared) + `isOwnedByWorkload` helper |
| `core/commands/scale.go`                        | `ScaleUpWorkloads`, `ScaleDownWorkloads`, `LogStartupOrder`, `CollectScaleTargets` (exported, with ownerReference filtering) — workload scaling and startup-order display (with configurable `scaleTimeout`)           |
| `core/commands/server_side_apply_annotation.go` | **New** — `ShouldServerSideApply` helper                                                                                                                                                                               |
| `core/k8s/apply.go`                             | `Apply` — updated with `serverSideApply` parameter                                                                                                                                                                     |
| `core/k8s/crds.go`                              | Extended with `WaitForCRDsEstablished`                                                                                                                                                                                 |
| `core/k8s/webhooks.go`                          | **New** — `DeleteWebhookConfigs`                                                                                                                                                                                       |
| `core/commands/webhook.go`                      | `ResolveWebhookProviders`, `WebhookProvider` type (no longer called by `bootstrapApply`)                                                                                                                               |
| `cli/action/cluster_apply.go`                   | Updated with the phase-based apply flow, `splitNamespaces` (note: `splitWebhooks` moved to `core/commands/delete.go` as exported `SplitWebhooks`)                                                                      |
| `cli/action/cluster_scale.go`                   | `ClusterScaleUp`, `ClusterScaleDown` action handlers                                                                                                                                                                   |
| `cli/cmd/cluster_scale.go`                      | cobra command registration for `hydra gitops scale up/down`                                                                                                                                                           |
| `core/commands/resolve.go`                      | `ResolveAppIdsFromConfig` — config-based wildcard resolution                                                                                                                                                           |
| `core/commands/resolve_test.go`                 | Unit tests for `ResolveAppIdsFromConfig`                                                                                                                                                                               |
| `core/commands/render.go`                       | `RenderCluster` and variants                                                                                                                                                                                           |
| `core/commands/cluster.go`                      | Cluster resolution helpers                                                                                                                                                                                             |
| `core/commands/list_cluster.go`                 | `ListCluster`, `ListClusterAll`, `ListClusterPredicate`                                                                                                                                                                |
| `core/commands/mark_as_selected.go`             | Selection/marking functions                                                                                                                                                                                            |
| `core/commands/namespace.go`                    | `ExclusiveNamespaces`, `CreateNamespaceEntities`                                                                                                                                                                       |
| `core/commands/visit.go`                        | `VisitResources` visitor pattern                                                                                                                                                                                       |
| `core/commands/backup.go`                       | Backup / restore command helpers including cert-manager-related backup flows                                                                                                                                           |
| `core/commands/scope_info.go`                   | `ScopeInfoMapFromCluster`, `ApplyScopeInfoMap`                                                                                                                                                                         |
| `core/commands/server_side_apply.go`            | Server-side apply utilities                                                                                                                                                                                            |
| `core/commands/uninstall_finalizer.go`          | `RemoveUninstallFinalizers`, `collectFinalizerPatches`                                                                                                                                                                 |
| `core/commands/bootstrap.go`                    | `ConvertSopsSecretsToSecrets`                                                                                                                                                                                          |
| `cli/cmd/test.go`                               | Top-level `test` command with subcommands                                                                                                                                                                              |
| `cli/cmd/test_refs.go`                          | `refs` subcommand for testing app-defined ref-parsers                                                                                                                                                                  |
| `cli/action/test_refs.go`                       | Action logic for `hydra local test refs` — loads chart values, runs golden file comparison                                                                                                                             |
