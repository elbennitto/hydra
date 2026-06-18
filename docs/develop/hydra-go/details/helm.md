# Helm Chart Processing Architecture

## Overview

The Helm package handles all aspects of Helm chart processing: loading charts from disk, downloading dependencies, rendering templates, merging values, and splitting manifests into individual YAML documents. It bridges between the Hydra context hierarchy and the entity system.

**Source files:** `core/helm/render.go`, `core/helm/values.go`, `core/helm/manifest.go`, `core/helm/chart_cache.go`, `core/helm/chartdirectory.go`, `core/helm/clone.go`, `core/helm/downloader.go`, `core/helm/hydra_fallback_values.go`

## Chart Loading

### LoadChart

```go
func (p PersistentChartDirectory) LoadChart(cache *ChartCache, mode types.NetworkMode) (chart.Charter, error)
```text

Method on `ChartDirectory` that loads a Helm chart from a directory with caching:

1. Check `ChartCache` for cached result (key: `path:networkMode`)
2. If cache miss: download dependencies (if needed), load chart from disk
3. Cache the result (both success and error cases)
4. Online mode results are also cached for offline mode

### ChartCache

```go
type ChartCache struct {
    mu    sync.RWMutex
    cache map[string]cacheEntry
}
```

Thread-safe chart cache:

- **Key:** `chartPath:networkMode`
- **Value:** `(*chart.Chart, error)` — both successful loads and errors are cached
- **Cross-mode caching:** Online results are stored under both `online` and `offline` keys

### Chart Directory Types

```go
type ChartDirectory interface {
    Path() string
    Cleanup() error
}
```text

| Type                       | Description                                                       |
| -------------------------- | ----------------------------------------------------------------- |
| `PersistentChartDirectory` | Regular chart directory (no cleanup)                              |
| `TemporaryChartDirectory`  | Temporary directory created for processing (cleaned up after use) |

`CreateTemporaryChartDirectory()` creates a temp dir in `.hydra/` for values processing when the original chart should not be modified.

## Dependency Management

### DownloadChartDependencies

```go
func DownloadChartDependencies(chartDir ChartDirectory, networkMode NetworkMode) error
```

Handles chart dependencies based on network mode:

```text
NetworkMode    Behavior
───────────    ────────
online         Download missing dependencies from remote repositories
offline        Use only locally available charts (fail if missing)
local          Resolve file:// dependencies locally, skip remote
```text

**Algorithm:**

1. Parse `Chart.yaml` to read dependency list
2. For each dependency:
   - If `repository` starts with `file://` → resolve local path
   - If `repository` is remote → download via Helm downloader (online mode only)
3. Place dependencies in `charts/` subdirectory

### Recursive Dependency Processing

```go
func dumpDependencies(chartDir, networkMode) error
```

Recursively processes all chart dependencies, including transitive dependencies. Each dependency's own dependencies are resolved before the parent chart is loaded.

## Values Processing

### LoadValuesMap

```go
func LoadValuesMap(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.ValuesMap, error)
```text

Loads and processes values for a loaded chart:

```text
Input values (from Hydra hierarchy)
  │
  ▼
1. Load chart from disk
  │
  ▼
2. Merge chart default values with input values
  │  Chart defaults are the base, input values override
  │
  ▼
3. Process dependencies
  │  Extract dependency-specific values from global values
  │
  ▼
4. Extract Hydra fallback values (from infra_library dependency)
  │  See "Hydra Fallback Values" below
  │
  ▼
5. Apply values-cleanup YQ expression (if configured)
  │  Runs a YQ expression to transform the merged values
  │
  ▼
6. `chartutil.ToRenderValues` (Helm render stage)
  │
  ▼
7. Optional YQ `values-cleanup` (if configured)
  │
  ▼
8. `MergeGlobalValues(..., "root")`
  │
  ▼
Final merged ValuesMap (post–`ToRenderValues`; suitable for inspection and `HydraApp.LoadValuesMap`, **not** for passing again into `helm.Template`; see "Coalesced values vs template input" below)
```

### CoalescedValuesMapBeforeRender (single Helm coalesce round)

```go
func CoalescedValuesMapBeforeRender(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.ValuesMap, error)
```text

Runs the **same** steps as the first part of `LoadValuesMap`:

1. `chartutil.CoalesceValues(ch, given)` — merge chart defaults with **user-supplied** `given`
2. `ProcessDependencies` — dependency and import-values handling
3. `extractHydraFallbackValues` — Hydra fallback from `infra_library`

It **stops before** `chartutil.ToRenderValues` and the YQ `values-cleanup` / `MergeGlobalValues("root")` tail of `LoadValuesMap`.

**Purpose:** this map matches what Helm’s install/template action expects as the **raw** values input for **one** coalesce round. `helm.Template` calls `action.Install.Run(chart, values)` with those user-style values; Helm performs coalescing internally.

### Why `helm.Template` must not use `LoadValuesMap` output as `ValuesMap`

Passing the **return value** of `LoadValuesMap` (already after `ToRenderValues`) into `helm.Template` means the values are processed **twice** through Helm’s coalesce/render pipeline. Nested overrides that only appear after dependency processing (for example `provider: webhook` plus extra value files for a child chart) can be **lost**, and charts may fall back to defaults (for example the `external-dns` chart defaulting to `aws`).

**Rule:** `RenderChartParams.ValuesMap` for `helm.Template` must be **install-style** values:

- **Root app:** merged GitOps/context/cluster `values.yaml` tree (`Cluster.LoadValuesMap`) plus the usual `global.hydra.cluster` injection where applicable.
- **Child app:** the same raw subtree Hydra builds for the child **before** the child’s own `helm.LoadValuesMap` — merged umbrella coalesced values, dependency `extraValueFiles`, etc. (`ChildApp.MergedChildValuesForHelmInstall`).

`HydraApp.LoadValuesMap()` remains the **fully merged** map (including `ToRenderValues`) for commands that show **effective** values (`hydra gitops values` uses `ClusterHelmInputValuesMap`, which starts from `LoadValuesMap`). That is **not** the same shape as the input to `helm.Template`.

**Cluster commands:** `hydra gitops template` registers a hook with `ClusterHelmInstallValuesMap` — same `global.hydra` replacement as `ClusterHelmInputValuesMap`, but applied on the **raw** install values described above. See `core/commands/cluster_helm_input_values.go` and `helmChartValuesForTemplate` in `core/hydra/helm_chart_template_values.go`.

**Child subtree source:** `LoadValuesFromRootApp` takes its parent slice from `RootApp.coalescedHelmValuesMap` (coalesced umbrella, pre–`ToRenderValues`) so extraction matches Helm’s dependency value layout.

### Values Merge Strategy

```go
func MergeValues(base, override types.ValuesMap) types.ValuesMap
```text

Deep merge with right-side precedence:

- **Maps:** Merged recursively (keys from both sides are kept)
- **Non-maps:** Override value replaces base value
- **Nil/missing:** Other side's value is used

```text
Example:
  base:     { a: { x: 1, y: 2 }, b: "old" }
  override: { a: { x: 9, z: 3 }, b: "new" }
  result:   { a: { x: 9, y: 2, z: 3 }, b: "new" }
```

### Hydra Fallback Values

```go
func extractHydraFallbackValues(chartDir, networkMode) (ValuesMap, error)
```text

Extracts fallback values from the `infra_library` chart dependency. This is a Hydra-specific mechanism for providing default values:

1. Find `infra_library` in chart dependencies
2. Clone the infra_library chart to a temporary directory
3. Add a special template that renders the fallback values
4. Render the chart with minimal values
5. Parse the rendered output as ValuesMap
6. Use these as additional default values

## Template Rendering

### Template

```go
func Template(params RenderChartParams) (YamlString, error)
```

Renders a Helm chart to a YAML manifest string:

```go
type RenderChartParams struct {
    KubernetesVersionOrFallback KubernetesVersionOrFallback
    ReleaseName                 string
    Namespace                   string
    ValuesMap                   ValuesMap
    SkipCrds                    bool
}
```text

**Implementation:**

1. Create a Helm `action.Install` client with `DryRunClient` strategy
2. Set release name, namespace, Kubernetes version, and `SkipCRDs` from `SkipCrds` (matches `helm template --skip-crds` / ArgoCD `helm.skipCrds`)
3. Run the install action (dry-run, no actual cluster changes)
4. Collect output from hooks (pre-install, post-install) and main manifest
5. Unless `SkipCrds` is true, append YAML from chart packaged CRD files (`chart.CRDs()` — the chart `crds/` directory)
6. Return concatenated YAML string

**Child apps:** `ChildApp` reads `<rootApp>.apps.<childApp>.skipCrds` from merged root values and passes it to `RenderChartParams.SkipCrds`, so local template output matches what ArgoCD applies when that flag is set. Root umbrella apps use `SkipCrds: false`.

**Child app static files** under `apps/<childApp>/` are concatenated after the Helm chart output. Files named `kubernetes-defaults-*.yaml` are treated as optional defaults: each YAML document in those files is dropped if its entity ID already appears in the Helm render or in non-defaults static files, so duplicates (for example Namespace and `default` ServiceAccount) are not merged twice.

**Synthetic namespace bundle:** `CreateNamespaceEntities` (used in `RenderCluster`) emits the same `kubernetes-defaults-<namespace>.yaml` shape without `appId`s. Before appending to the selected template render, documents whose IDs already exist in the rendered templates are dropped so they do not duplicate chart or static output.

### TemplateV2Chart

For v2 (legacy) charts:

```go
func TemplateV2Chart(params RenderChartParams) (YamlString, error)
```

Delegates to `templateV2Chart` which handles the actual Helm template rendering for v2 charts.

## Manifest Splitting

### SplitManifestMap

```go
func SplitManifestMap(manifest YamlString) map[string][]YamlString
```text

Splits a rendered manifest into individual YAML documents organized by source template:

```text
Input:
  ---
  # Source: my-chart/templates/deployment.yaml
  apiVersion: apps/v1
  kind: Deployment
  ...
  ---
  # Source: my-chart/templates/configmaps.yaml
  apiVersion: v1
  kind: ConfigMap
  ...
  ---
  # Source: my-chart/templates/configmaps.yaml
  apiVersion: v1
  kind: ConfigMap
  ...

Output:
  map[string][]YamlString{
    "my-chart/templates/deployment.yaml":  [deployment-yaml],
    "my-chart/templates/configmaps.yaml":  [configmap1-yaml, configmap2-yaml],
  }
```

**Algorithm:**

1. Split manifest by `---` separator
2. For each document:
   - Look for `# Source: <path>` comment at the top
   - If found: use path as the map key
   - If not found: assign `unknown-N.yaml` as key
3. Return map from template path to ordered list of YAML documents

The order within each template path is preserved. The `templateIndex` (1-based) is derived from the position in the list.

## Data Flow

```text
Hydra Context / RootApp / ChildApp
  │
  ├── LoadValuesMap()  (inspection / second-stage merged values)
  │     Hierarchical user values → chart Coalesce + ToRenderValues + cleanup
  │
  ├── helm.Template / child Template path
  │     Raw install-style ValuesMap only (see CoalescedValuesMapBeforeRender + MergedChildValuesForHelmInstall)
  │
  ▼
helm.LoadChart(chartDir, networkMode)
  │  Load chart + download dependencies
  │  Cached by path + networkMode
  │
  ▼
helm.LoadValuesMap(chartDir, inputValues, networkMode)
  │  Merge chart defaults + input values
  │  Process dependencies + fallback values
  │  Apply YQ cleanup
  │
  ▼
helm.Template(chart, values, releaseName, namespace, k8sVersion)
  │  Render with Helm engine
  │  Include CRDs and hooks
  │
  ▼
YamlString (rendered manifest with # Source: comments)
  │
  ▼
helm.SplitManifestMap(manifest)
  │  Split by --- separator
  │  Extract # Source: paths
  │
  ▼
map[templatePath][]YamlString
  │
  ▼
entity.NewEntitiesFromYaml(l, manifest, key)
  │  Create Entity objects with templatePath, templateIndex, and unstructured resource
  │
  ▼
Entities
```text

## ArgoCD Integration

After rendering, templates are patched with ArgoCD tracking annotations:

```go
func YqPatchArgo(manifest YamlString, appId AppId, namespace Namespace) (YamlString, error)
```

Adds `argocd.argoproj.io/tracking-id` annotation to each resource:

```yaml
metadata:
  annotations:
    argocd.argoproj.io/tracking-id: "production.monitoring.prometheus:apps/Deployment:monitoring/prometheus"
```text

Format: `{appId}:{group}/{kind}:{namespace}/{name}`

**Rules:**

- CRDs are skipped (ArgoCD tracks them differently)
- Resources with existing tracking-id are not overwritten
- Tracking-id set to `"none"` causes removal of the annotation
