# Values Pipeline Architecture

## Overview

The values pipeline handles loading, merging, and processing of Helm values throughout the Hydra context hierarchy. Values are loaded from multiple YAML files at different levels (context, cluster, root app, child app) and deep-merged to produce the final values for chart rendering.

**Source files:** `core/values/values.go`, `core/helm/values.go`, `core/hydra/hydra_values.go`

## Values Hierarchy

Values are loaded bottom-up through the Hydra context hierarchy, with each level adding its own values on top:

```text
Level        Source files                              Merge order
─────        ────────────                              ───────────
Context      group-*/values.yaml                       1. Groups (alphabetical)
             <context>/values.yaml                     2. Context overrides groups

Cluster      (all Context values)                      3. Context as base
             in-cluster/values.yaml                    4. Shared cluster values
             <cluster>/values.yaml                     5. Cluster-specific overrides

RootApp      (all Cluster values)                      6. Cluster as base
             <root-app>/values.yaml                    7. Root app overrides

ChildApp     (extracted from RootApp values)           8. Global values as base
             child-specific overrides                  9. Child-specific overrides
             extra value files                         10. Extra files override
```

## Core Functions

### values.LoadValuesFile

```go
func LoadValuesFile(l log.Logger, path string) (types.ValuesMap, error)
```

Loads a single YAML values file. Returns an empty `ValuesMap` if the file does not exist (not an error). This allows optional values files at every level.

### values.LoadAndMergeValuesFile

```go
func LoadAndMergeValuesFile(l log.Logger, filePath string, existingValues types.ValuesMap) (types.ValuesMap, error)
```

Loads a values file and merges it with existing values. The file's values **override** the existing values (right-side precedence).

### values.MergeValues

```go
func MergeValues(base, override ValuesMap) ValuesMap
```

Deep-merges two ValuesMaps:

```text
Rule 1: Maps are merged recursively
  base:     { a: { x: 1, y: 2 } }
  override: { a: { x: 9, z: 3 } }
  result:   { a: { x: 9, y: 2, z: 3 } }

Rule 2: Non-map values are replaced
  base:     { a: "old", b: [1, 2] }
  override: { a: "new", b: [3] }
  result:   { a: "new", b: [3] }

Rule 3: Nil/missing values use the other side
  base:     { a: 1, b: 2 }
  override: { b: 3, c: 4 }
  result:   { a: 1, b: 3, c: 4 }
```

### values.MergeGlobalValues

```go
func MergeGlobalValues(values types.ValuesMap, appKey string) (types.ValuesMap, error)
```

Ensures global values are available to all subchart dependencies. Copies the `global` key from the top-level values into each dependency's values. The `appKey` parameter identifies the app for dependency-specific value extraction.

### values.Lookup

```go
func Lookup(values ValuesMap, keys ...string) (any, error)
```

Nil-safe traversal of nested maps with variadic keys:

```go
// Equivalent to values["hydra"]["kubectl"]["contexts"]
result, err := values.Lookup(values, "hydra", "kubectl", "contexts")
```

Returns an error if any intermediate key is missing or not a map.

### values.ParseValuesString

```go
func ParseValuesString(yaml YamlString) (ValuesMap, error)
```

Parses a YAML string directly to a `ValuesMap`.

## Helm Values Processing

### helm.LoadValuesMap

```go
func LoadValuesMap(l log.Logger, ch chart.Charter, given types.ValuesMap) (types.ValuesMap, error)
```

Processes values for chart rendering. Takes a loaded chart (not a directory) and the given values from the Hydra hierarchy. This is the main values processing function that combines multiple sources:

```text
Input values (from Hydra hierarchy)
  │
  ▼
1. Load chart from disk
  │  Read Chart.yaml for chart metadata and defaults
  │
  ▼
2. Merge chart default values with input values
  │  base: chart.Values (defaults from values.yaml in chart)
  │  override: inputValues (from Hydra hierarchy)
  │
  ▼
3. Process dependencies
  │  For each chart dependency:
  │    Extract dependency-specific values from merged values
  │    Apply alias/condition rules per Helm spec
  │
  ▼
4. Extract Hydra fallback values
  │  If infra_library dependency exists:
  │    Clone chart, add extraction template, render, parse
  │    Merge fallback values as additional defaults
  │  (See HELM.md for details)
  │
  ▼
5. Apply values-cleanup YQ expression
  │  If HydraValues.ValuesCleanup is set:
  │    Run YQ expression to transform the merged values
  │    Used for removing temporary/internal values
  │
  ▼
6. Merge global values
  │  Copy top-level "global" key into each dependency's values
  │  Ensures subchart dependencies have access to global values
  │
  ▼
Final ValuesMap (ready for helm.Template)
```

## Hydra Values Extraction

### HydraValues

```go
func HydraValues(h Hydra) (*types.HydraValues, error)
```

Extracts Hydra-specific configuration from the Hydra context's merged values:

```go
type HydraValues struct {
    KubernetesVersion     KubernetesVersion            `yaml:"kubernetesVersion"`
    AdditionalSourceRepos []string                     `yaml:"additionalSourceRepos"`
    Cluster               string                       `yaml:"cluster"`
    Path                  string                       `yaml:"path"`
    Repository            string                       `yaml:"repository"`
    Revision              string                       `yaml:"revision"`
    Stage                 string                       `yaml:"stage"`
    KubeCtl               HydraKubectl                 `yaml:"kubectl"`
    Refs                  map[string]HydraRefGroup     `yaml:"refs"`
    UninstallFinalizer    []string                     `yaml:"uninstall-finalizer"`
    Scale                 map[string]HydraScaleGroup   `yaml:"scale"`
    Ready                 map[string]HydraReadyGroup   `yaml:"ready"`
}

type HydraReadyGroup struct {
    Predicate string   `yaml:"predicate"` // CEL: entity matches this rule when true
    Cel       []string `yaml:"cel"`       // each entry: empty string = pass; non-empty = failure reason; bool supported
}

type HydraScaleGroup struct {
    GVK             string   `yaml:"gvk"`
    ReplicaPaths    []string `yaml:"replicaPaths"`
    StatusReadyPath string   `yaml:"statusReadyPath,omitempty"`
}
```

**Validation (in `HydraValues.Validate()`):**

- `replicaPaths` must not be empty → error: _"hydra configuration validation failed: scale.{name}.replicaPaths must have at least one entry"_
- `gvk` must be a valid GVK string (format: `group/version/Kind` or `version/Kind`) → error: _"hydra configuration validation failed: scale.{name}.gvk is invalid"_
- Duplicate GVK strings within the same app's scale definitions → error: _"hydra configuration validation failed: duplicate scale GVK {gvk}"_

```go
type HydraRefGroup struct {
    Tag        []string               `yaml:"tag"`
    Attributes []RefParserAttribute     `yaml:"attributes,omitempty"`
    Desc       string                 `yaml:"desc,omitempty"`
    Label      string                 `yaml:"label,omitempty"`
    Reverse    bool                   `yaml:"reverse,omitempty"`
    Enabled    *bool                  `yaml:"enabled,omitempty"`
    RefParsers []HydraRefParser       `yaml:"ref-parsers"`
}

type HydraRefParser struct {
    Predicate  string                 `yaml:"predicate"`
    Pick       []HydraRefPick         `yaml:"pick"`
    Attributes []RefParserAttribute   `yaml:"attributes,omitempty"`
    Tag        []string               `yaml:"tag,omitempty"`
    Label      string                 `yaml:"label,omitempty"`
    Reverse    bool                   `yaml:"reverse,omitempty"`
}

type HydraRefPick struct {
    Cel        string                 `yaml:"cel"`
    Attributes []RefParserAttribute   `yaml:"attributes,omitempty"`
    Tag        []string               `yaml:"tag,omitempty"`
    Label      string                 `yaml:"label,omitempty"`
    Reverse    bool                   `yaml:"reverse,omitempty"`
}
```

**`HydraRefGroup.attributes`** — Optional structured attributes merged onto every parser in the group. For edges that declare **runtime-materialized** targets (not present in plain Helm output), declare **`"origin:generated": job`** (e.g. password `Job` → `Secret`) or **`"origin:generated": controller`** (e.g. operator/CR → `Secret`/`ConfigMap`). Uninstall-related semantics continue to use **`tag`** (for example `uninstall`, `uninstall-force`), and group-level **`priority`** provides the default ownership strength for all parsers in that group. For how **`label`**, **`tag`**, **`attributes`**, **`priority`**, and **`reverse`** differ, and when to use **`origin:app`** / **`origin:workload`**, see [references.md](references.md#semantic-roles-of-labels-tags-attributes-and-reverse) and [Origin and provenance attributes](references.md#origin-and-provenance-attributes).

**Prefer YAML `attributes` for `origin:generated`** — Declare **`"origin:generated": job`** / **`"origin:generated": controller`** in **`HydraRefGroup.attributes`** or **`HydraRefParser.attributes`** (YAML under `global.hydra.refs`), not via CEL `refBuilder()...attribute("origin:generated", ...)`. That keeps materialization policy declarative and avoids duplicating the same value in every `pick` line. Use CEL `.attribute("origin:generated", ...)` only when a single parser entry emits refs that need **different** `origin:generated` values—in that case **split** the entry into two YAML `ref-parsers` rows (same `predicate` allowed) so each row has its own `attributes` list.

The values are looked up under the `global.hydra` key in the merged values:

```yaml
global:
  hydra:
    kubectl:
      allowedContexts:
        - name: production
          cluster: prod-cluster
          authInfo: prod-user
    refs:
      image-pull-secrets:
        tag: [kyverno]
        desc: "Image pull secrets cloned from sops-secrets-operator"
        label: clone-source
        ref-parsers:
          - predicate: 'gvk == "v1/Secret" && name == "image-pull-secret" && ns != "sops-secrets-operator"'
            pick:
              - cel: '[refBuilder().outgoing(id("v1/Secret", "sops-secrets-operator", "image-pull-secret"))]'
      managed-resources:
        tag: [kyverno, uninstall]
        desc: "Resources with kyverno managed-by labels"
        ref-parsers:
          - predicate: 'clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "kyverno"'
            pick:
              - cel: '[refBuilder().outgoing(ref("provider", "kyverno"))]'
    uninstall-finalizer:
      - "argocd.argoproj.io/hook-finalizer"
    scale:
      strimzi-kafka:
        gvk: "kafka.strimzi.io/v1beta2/Kafka"
        replicaPaths:
          - "spec.kafka.replicas"
          - "spec.zookeeper.replicas"
        statusReadyPath: "status.listeners"
      strimzi-connect:
        gvk: "kafka.strimzi.io/v1beta2/KafkaConnect"
        replicaPaths:
          - "spec.replicas"
        statusReadyPath: "status.readyReplicas"
    kubernetesVersion: "1.28"
```

### `global.hydra.ready` (readiness rules for scale status and scale up)

**Placement and merge:** `global.hydra.ready` lives under the same `global.hydra` namespace as `refs` and `scale`. It is loaded from Helm values through the **same Hydra values hierarchy and deep-merge** as other `global.hydra` keys (context → cluster → root app → child overrides). Map keys are **rule names** (identifiers for operators, like `scale` / `refs` group names).

**Shape (per named entry):**

- **`predicate`** — CEL expression that must evaluate to **boolean `true`** for an entity to **fall under** this rule. Uses the **same entity variable contract** as ref-parser predicates and CLI `--include` / `--exclude` filters (`gvk`, `ns`, `name`, `entity`, `kind`, `group`, `version`, etc.). See [details/cel.md — Available Variables in Ref-Parsers](cel.md#available-variables-in-ref-parsers).
- **`cel`** — **YAML list** of CEL expressions. Each entry must evaluate to a value that **passes** that check (see tri-state rules below). **All** checks must pass → the entity is **ready** for this rule. A **null** or omitted value for an expression **omits** that check (unchanged from “no contribution”). **`bool` is not accepted**; use `""` / non-empty strings or lists of strings instead.

**Selection when no rule matches:** If **no** named rule’s `predicate` matches an entity, that entity is **not selected** for ready display or for ready-based gating: it does **not** appear in ready-oriented columns or dependency rows, and scale-up does **not** wait on it for ready semantics (only the existing sync/replica polling applies).

**Multiple predicates true:** If more than one rule’s `predicate` matches the same entity, the implementation must apply a **deterministic tie-break** (for example a single winning rule chosen by sorted rule name). Document the exact choice in implementation notes; operators should write predicates that do not overlap in practice.

#### Tri-state and non-boolean outcomes

Ready evaluation expects **`string`**, **`list(string)`**, or **`null`** per `cel` entry: **null** skips the check; **`""`** or **`[]`** passes; a **non-empty string** or a **non-empty list of non-empty strings** fails and contributes to `readyMessages` (lists are flattened in order). **Any other type (including `bool`)** is an **evaluation error** and is treated as not ready with an error-derived message. **Evaluation errors** are also treated as not ready with an error-derived message in `hydra gitops scale status` (`readyMessages`).

**Live inventory in CEL:** When the CLI builds a ready evaluator with merged template+cluster entities, expressions may call **`templateEntities()`**, **`clusterEntities()`**, **`entities()`**, their selector-object overloads, **`managedNamespaces()`**, and **`involvedObjectEvents(limit, kind, name, namespace)`** without additional Kubernetes list calls. See [details/cel.md — global.hydra.ready rules](cel.md#global-hydra-ready-rules-predicate-and-cel-list).

**Interaction with built-in defaults:** The product supplies **built-in default ready rules** for standard scale workload kinds and for GVKs registered in `global.hydra.scale`, so `hydra gitops scale status` keeps workload visibility when users add **no** custom `global.hydra.ready` entries. User-defined rules **merge** with that baseline per the values pipeline; overriding or extending behavior is by map key and predicate design (see [Built-in default ready rules](#built-in-default-ready-rules) below).

**Example (custom conditions check via CEL):** Users can encode `status.conditions` style logic in the `cel` list. One approach is to combine list emptiness and per-condition status in CEL (exact helpers depend on the CEL list API bound in Hydra’s env):

```yaml
global:
  hydra:
    ready:
      my-cr-ready:
        predicate: 'gvk == "example.com/v1/MyCR"'
        cel:
          - 'size(entity.status.conditions) > 0 ? "" : "status.conditions is empty"'
          - '!entity.status.conditions.exists(c, c.status == "False") ? "" : "a condition has status False"'
          - '!entity.status.conditions.exists(c, c.status != "True" && c.status != "False") ? "" : "a condition has an unknown status"'
```

(Adjust field access if your CRD uses different condition shapes; the intent is: non-empty conditions, no `False`, no unknown status values if you require strict `True` only.)

#### Built-in default ready rules

These defaults are **shipped with the product** (not empty unless explicitly disabled by a future flag—document implementation if added). They cover standard **scale target** kinds (`CollectScaleTargets`) plus **`v1/Pod`** for **scale status** dependency rows and **scale-up** ready gating when Pods appear on the ref graph, and **`v1/Secret`** / **`v1/ConfigMap`** when those resources appear on the same dependency graph (presence check via **`clusterEntities()`** and **`id`**).

| Workload class | Default ready intent (conceptual) |
| -------------- | --------------------------------- |
| **Deployment** | When desired replicas > 0: **`status.conditions`** — **ReplicaFailure** **True**, **Available** **False**/**Unknown**, **Progressing** **False**, plus other **False**/**Unknown** types generically; then **readyReplicas** vs desired (same spirit as scale-up `waitReady`). |
| **StatefulSet** | Same **`status.conditions`** pattern as Deployment (**ReplicaFailure**, **Available**, **Progressing**, catch-all), then **readyReplicas** vs desired. |
| **ReplicaSet** | Core API has no standard condition types; if **`status.conditions`** is present, any **False**/**Unknown** entry fails with a generic message; then **readyReplicas** vs desired. |
| **DaemonSet** | When **desiredNumberScheduled** > 0: same condition-style checks as Deployment where present, then **numberReady** vs **desiredNumberScheduled**. |
| **Pod (`v1/Pod`)** | For **Pending**/**Running**, built-in rules surface **False**/**Unknown** on standard `status.conditions` types (**PodScheduled**, **Initialized**, **ContainersReady**, **Ready**, **PodReadyToStartContainers**), **DisruptionTarget** **True**, plus other failing types generically; **Succeeded**/**Failed** use phase summary. Container **waiting** reasons (image pull, config error, crash loop) are checked separately. |
| **Secret / ConfigMap (`v1/Secret`, `v1/ConfigMap`)** | **`clusterEntities()`** contains an entry whose **`id`** equals the entity’s id (**`""`** pass, **`missing`** failure). |
| **Job** | Not ready if **`Failed`** or **`FailureTarget`** is **True** (Kubernetes may set **FailureTarget** around backoff-limit failure before or alongside **Failed**); **`not_ready`** messages prefer **`status.conditions`** **`message`**, else **`reason`**, else a generic **job failed** ( **`Failed`** is preferred over **`FailureTarget`** when both exist). Ready when **Complete** is True or succeeded count meets completions. |
| **CronJob (`batch/v1/CronJob`)** | Not a `CollectScaleTargets` root today, but may appear on ref graphs. **`CronJobStatus`** has no **`conditions`** in core Kubernetes; built-in rule treats **`spec.suspend` `true`** as not ready (no new Jobs scheduled). |
| **Custom `global.hydra.scale` targets** | When `statusReadyPath` is set, non-empty/non-nil at that path means ready (consistent with existing scale-up polling). When `statusReadyPath` is absent, default ready rule may mirror the **fire-and-forget** posture (no ready signal) or a conservative replica-only check—**document the exact mapping in implementation** next to `HydraScaleGroup` handling. |

Defaults preserve **existing** `scale status` workload rows and add a **ready** dimension only where a rule exists (built-in or user-defined).

---

**`global.hydra.scale`** declares custom CRD-based resources as scale targets for `hydra gitops scale up/down`. Each named entry specifies a GVK string and a list of JSONPath-like dot-separated paths to replica fields in the resource spec. The key (e.g., `strimzi-kafka`) is for documentation/identification only.

`statusReadyPath` is optional. Used during scale-up polling to determine readiness. If provided, the field at this path is checked: for scale-up, a non-nil/non-empty value means ready; for scale-down, a nil/zero value means scaled down. If not provided, polling is skipped for custom workloads (fire-and-forget — the operator handles reconciliation).

Scale definitions are configured in the app that ships the CRD (e.g., `strimzi-cluster-operator`) but apply globally to all apps. When `hydra gitops scale` or `hydra gitops apply` runs with a selected app set, Hydra builds the effective scale map by merging **`global.hydra.scale`** the same way as ref-parsers: cluster-level values, then each selected app’s Helm `global.hydra` merged with **`data.hydra` Hydra ConfigMap** fragments from the rendered manifests, then app-independent global ConfigMap fragments. Later sources override earlier entries for the same GVK. Matching CRD-based resources are collected alongside built-in workload types (Deployment, StatefulSet, ReplicaSet, DaemonSet).

See [commands.md — Data Flow (Cluster Scale)](commands.md#data-flow-cluster-scale) for how custom scale workloads are integrated into the scale data flow.

See [references.md — Tags, Desc, and Label](references.md#tags-desc-and-label) for detailed documentation of tags (including uninstall tags) and [references.md — App-Defined Ref-Parsers](references.md#app-defined-ref-parsers) for the ref-parser configuration format.

## Child App Values

Child app values are extracted from the root app's rendered values. The root app contains configuration for all its children:

Note on fallback files: Hydra fallback values are extracted from the `infra_library` dependency during chart values processing. Since `infra_library` is usually part of root app chart dependencies, cluster dumps commonly contain fallback files for root app IDs only. Child apps therefore may have merged values without having their own dedicated fallback file in `values/fallback/`.

```yaml
# Root app values.yaml
global:
  domain: example.com

# Child-specific values (under the child app name)
prometheus:
  retention: 30d
  replicas: 2

# Hydra child app configuration
hydra:
  apps:
    prometheus:
      namespace: monitoring
      extraValues:
        - path/to/extra-values.yaml
```

### LoadValuesFromRootApp

```go
func (a *ChildApp) LoadValuesFromRootApp(networkMode types.NetworkMode) (types.ValuesMap, []string, error)
```

Returns the merged values and a list of extra value file paths (used as **user** input to the child chart’s `helm.LoadValuesMap`, and as the basis for `MergedChildValuesForHelmInstall`).

1. Load root umbrella values **after one Coalesce round** (`RootApp.coalescedHelmValuesMap` — same stage as `helm.CoalescedValuesMapBeforeRender`, **not** full `RootApp.LoadValuesMap` / `ToRenderValues`)
2. Extract `global` values
3. Extract child-specific values (under child app name key)
4. Merge: global as base, child-specific overrides
5. Resolve `extraValueFiles` from `<rootApp>.apps.<childApp>`; file list is returned for step 6 in the caller (`child_app.go`)
6. Caller merges each extra file from the child chart directory (`LoadAndMergeValuesFile`)

Rationale: the child chart subtree must match Helm’s dependency values **before** the umbrella’s render pass mutates shape; see [helm.md](helm.md) (“Coalesced values vs template input”).

## Data Flow

```text
Context values files
  │
  ▼ values.LoadAndMergeValuesFile (per file)
  │
Context ValuesMap
  │
  ▼ values.MergeValues (+ cluster files)
  │
Cluster ValuesMap
  │
  ▼ values.MergeValues (+ root app files)
  │
RootApp ValuesMap
  │
  ├── HydraValues() → extract Hydra config
  │
  ├── For RootApp rendering:
  │   ▼ helm.Template: Cluster.LoadValuesMap (plus global.hydra patches) — install-style raw values
  │   │
  │   Separate path: helm.LoadValuesMap → final merged ValuesMap (inspection / hydra gitops values)
  │
  └── For ChildApp:
      ▼ LoadValuesFromRootApp() → + extra value files → raw user values for child chart
      │
      ├── helm.Template: MergedChildValuesForHelmInstall() (and cluster hook: ClusterHelmInstallValuesMap)
      │
      └── helm.LoadValuesMap (+ child chart defaults) → ChildApp.LoadValuesMap() for inspection
```

## Value Files Export

The cluster dump exports all values files from the GitOps repository that are **not** bundled inside chart `.tgz` archives. These are tracked in `hydra.yaml` and copied to the `values/` directory.

### Collected Value Files

| Level   | Source Path (relative to context parent)                         | Type      |
| ------- | ---------------------------------------------------------------- | --------- |
| Group   | `values.yaml`                                                    | `group`   |
| Context | `<context>/values.yaml`                                          | `context` |
| Cluster | `<context>/<cluster>/values.yaml`                                | `cluster` |
| App     | Extra value files for child apps (from `extraValueFiles` config) | `app`     |

Root app chart `values.yaml` files are also tracked as type `app` for completeness (even though they are bundled in the chart `.tgz`).

### Collection Logic

**Source:** `core/commands/values_info.go`

- `CollectValueFiles(cluster, appIds, networkMode)` — Walks the values hierarchy, reads file paths, and returns `[]ValueFileModel` with relative paths from the context parent directory.
- `CollectAppValues(cluster, appIds, networkMode)` — Calls `LoadValuesMap()` for each app and returns the fully merged values.

### Export

**Source:** `core/export/export.go`

- `WriteValueFiles(l, dir, valueFiles, contextParentPath)` — Copies source files to `<dir>/values/files/<path>`.
- `WriteMergedValues(l, dir, appValues)` — Writes merged values as `<dir>/values/merged/<appId>.yaml`.

See also: `../../shared/details/cluster-dump-structure.md` for directory layout.

## ValuesMap Type

```go
type ValuesMap = map[string]any
```

A type alias for `map[string]any`, used throughout the system for representing YAML values. Values can be:

- `string`, `int`, `float64`, `bool` — scalar values
- `[]any` — lists
- `map[string]any` — nested maps (recursively)

The deep-merge algorithm handles all these types, only recursing into `map[string]any` values.
