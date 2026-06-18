# CEL Expression Architecture

## Overview

Hydra uses the Common Expression Language (CEL) for three purposes: discovering references between entities (ref-parsers), filtering/selecting entities (predicates), and projecting rendered entities to YAML-serializable values for local query commands such as `hydra local find`. The CEL package provides a configured environment with custom functions, entity support, and program compilation.

**Source files:** `core/cel/env.go`, `core/cel/expression.go`, `core/cel/predicate.go`, `core/cel/program.go`, `core/cel/programs.go`, `core/cel/entity_support.go`, `core/cel/service_support.go`, `core/cel/util_support.go`, `core/cel/list_support.go`, `core/cel/ref_type.go`, `core/cel/value_type.go`

## CEL Environment

### Setup

```go
func NewEnv(extraOptions ...cel.EnvOption) (Env, error)
```

Creates a configured CEL environment with all extensions and custom functions. Extra options (e.g. `ListSupport`, `ServiceSupport`) can be passed to extend the environment:

```text
CEL Environment
├── Standard extensions
│   ├── Encoders (base64, URL encoding)
│   ├── Strings (trim, replace, split, etc.)
│   ├── Lists (flatten, range, filter, etc.)
│   ├── Regex (match, capture)
│   ├── Math (min, max, ceil, floor)
│   └── Sets (contains, intersect, equivalent)
│
├── Entity support
│   ├── Entity field readers (all EntityKey types)
│   ├── annotations() → map access
│   ├── labels() → map access
│   └── getOrEmpty() → nil-safe map access
│
├── Utility support
│   ├── id(gvk, namespace, name) → creates entity ID
│   └── ref(type, value) → creates custom endpoint
│
├── Ref-builder support
│   ├── refBuilder() → creates RefDefinition builder
│   ├── .incoming(endpoint) → adds incoming endpoint
│   ├── .outgoing(endpoint) → adds outgoing endpoint
│   ├── .label(string) → sets label
│   └── .reverse() → marks as reversed
│
└── Service support
    └── matchingServices() → find services by label selector
```

### Entity to CEL Conversion

```go
func entityToMap(entity Entity) map[string]any
```

Converts an Entity to a flat map for CEL evaluation. All `EntityKey` values are converted to their string/bool/int representations and added with the key name as map key. The full unstructured resource is available as `entity` for accessing `spec`, `metadata`, etc.

## Expressions

### Expression

```go
type Expression interface {
    Expression() types.CelExpression
    Eval(e entity.Entity) (ref.Val, error)
}
```

Evaluates a CEL expression against an entity and returns any value. Used for ref-parser `pick` expressions that return `RefDefinition` objects and for `hydra local find --pick` expressions that return YAML-serializable scalars, lists, or maps.

### Predicate

```go
type Predicate interface {
    Predicate() []types.CelPredicate
    Select(e entity.Entities) (entity.Entities, entity.Entities, error)
    EvalBool(e entity.Entity, missingKeys types.MissingKeys) (bool, error)
}
```

Evaluates a CEL expression that returns a boolean. Used for entity filtering and selection. `Select` returns `(allWithMarks, matchedOnly, error)`. `EvalBool` evaluates against a single entity — the `missingKeys` parameter controls behavior when entity keys are missing (`MissingKeysReject` returns false, `MissingKeysAccept` returns true).

### Programs (Multiple Predicates)

```go
type programs struct {
    env      *Env
    programs []program
}
```

Combines multiple predicates with AND logic. An entity matches only if **all** predicates return true.

```go
func (e *Env) CompilePredicate(predicates ...types.CelPredicate) (Predicate, error)
```

Compiles one or more CEL predicate strings. If multiple expressions are provided, they are combined with AND logic.

## Compilation

```go
func (e *Env) CompileExpression(expression types.CelExpression) (Expression, error)
func (e *Env) CompilePredicate(predicates ...types.CelPredicate) (Predicate, error)
```

Both are methods on `Env`. They:

1. Parse the CEL expression string
2. Check for type errors
3. Compile to a CEL program
4. Return a wrapper that evaluates against entities

## Custom Functions

### Entity Functions

| Function        | Signature                      | Description                                       |
| --------------- | ------------------------------ | ------------------------------------------------- |
| `annotations()` | `entity.annotations() → map`   | Access metadata annotations                       |
| `labels()`      | `entity.labels() → map`        | Access metadata labels                            |
| `getOrEmpty()`  | `map.getOrEmpty(key) → string` | Nil-safe map access (returns `""` if key missing) |

### Reference Builder Functions

| Function              | Signature                                 | Description                              |
| --------------------- | ----------------------------------------- | ---------------------------------------- |
| `refBuilder()`        | `refBuilder() → RefBuilder`               | Creates new reference definition builder |
| `.incoming(endpoint)` | `builder.incoming(endpoint) → RefBuilder` | Adds incoming endpoint                   |
| `.outgoing(endpoint)` | `builder.outgoing(endpoint) → RefBuilder` | Adds outgoing endpoint                   |
| `.label(string)`      | `builder.label(string) → RefBuilder`      | Sets label on last endpoint              |
| `.reverse()`          | `builder.reverse() → RefBuilder`          | Marks last def as reversed               |

### ID and Ref Functions

| Function | Signature                                | Description                   |
| -------- | ---------------------------------------- | ----------------------------- |
| `id()`   | `id(gvk, namespace, name) → RefEndpoint` | Creates ID-based endpoint     |
| `ref()`  | `ref(type, value) → RefEndpoint`         | Creates custom-typed endpoint |
| `objectsetRioOwnerGvkToHydraGvk()` | `objectsetRioOwnerGvkToHydraGvk(string) → string` | Parses **`objectset.rio.cattle.io/owner-gvk`** values (`/v1, Kind=Service`, `apps/v1, Kind=Deployment`, …) into Hydra **`gvk`** strings for **`id()`** (returns `""` when invalid); see [Rancher objectset owner annotations](references.md#rancher-objectset-owner-annotations) and `core/cel/objectset_rio_owner_gvk.go` |

### Service Functions

| Function             | Signature                                   | Description                                        |
| -------------------- | ------------------------------------------- | -------------------------------------------------- |
| `matchingServices()` | `matchingServices(entity, services) → list` | Find services whose selector matches entity labels |

## Usage in Ref-Parsers

Ref-parsers use CEL for both matching (predicate) and extraction (expression):

```yaml
ref-parsers:
  - predicate: "gvk == 'apps/v1/Deployment'"
    pick:
      - "refBuilder().outgoing(id('v1/ServiceAccount', ns, entity.spec.template.spec.serviceAccountName)).label('serviceAccount')"
```

### Available Variables in Ref-Parsers

| Variable  | Type           | Source                                   |
| --------- | -------------- | ---------------------------------------- |
| `gvk`     | `string`       | Entity GVK (e.g. `"apps/v1/Deployment"`) |
| `group`   | `string`       | API group                                |
| `version` | `string`       | API version                              |
| `kind`    | `string`       | Resource kind                            |
| `ns`      | `string`       | Namespace                                |
| `name`    | `string`       | Resource name                            |
| `entity`  | `map`          | Full unstructured resource               |
| `CRDs`    | `list[string]` | All GVK strings from CRDs                |

### global.hydra.ready rules (predicate and cel list)

Each named entry under **`global.hydra.ready`** uses the **same entity binding** as ref-parser `predicate` rows: `gvk`, `group`, `version`, `kind`, `ns`, `name`, `entity` (full unstructured map), and other keys exposed by `entityToMap` for the evaluation context. The **`predicate`** selects which entities the rule applies to. Each string in the **`cel`** list is evaluated independently against the same entity. **Every** expression must **pass** for the entity to count as **ready**. Pass/fail semantics per expression:

- **`null`** — omit this check (no contribution).
- **`""`** — pass.
- **Non-empty `string`** — fail; the string is one `readyMessages` entry.
- **`list(string)`** — fail if the list contains any non-empty string after trimming empty elements; each non-empty element becomes one `readyMessages` entry. An **empty** list passes (same as `""`).
- **Any other type (including `bool`)** — evaluation error for that rule; the error is surfaced as `not_ready` with an error-derived message.

**Entity inventory helpers** (registered with `ClusterInventorySupport` / `NewEnvWithEntityInventory`) expose rendered and live snapshots as lists of maps (same keys as `entityToMap`). Overloads:

- **`templateEntities()`** / **`templateEntities(selector)`** — entities that have `templateEntity`. The selector is an object using Hydra resource-selector fields such as **`group`**, **`version`**, **`kind`**, **`apiVersion`**, **`gvk`**, **`namespace`** (or alias **`ns`**), **`gvkn`**, **`name`**, and **`id`**. Namespace matching keeps namespaced rows with `ns == namespace`, and also matches `v1/Namespace` rows whose `name` equals the requested namespace.
- **`clusterEntities()`** / **`clusterEntities(selector)`** — same for `clusterEntity`.
- **`entities()`** / **`entities(selector)`** — concatenation of template snapshots then cluster snapshots (optional selector applied to each side).
- **`managedNamespaces()`** — `list(string)` of sorted unique namespace names derived from the **full** entity set passed into inventory setup (namespaced `ns` values plus `v1/Namespace` `name` values), matching the former `HydraManagedNamespaces` list.

**`global.hydra.ready`** evaluators use merged template+cluster entities when available (for example `hydra gitops scale` / `scale status` after render/list merge), so **`templateEntities()`** and **`clusterEntities()`** can both be non-empty. Ref-parser and clone CEL environments register the same helpers from the entity set in scope (often template-only).

**`involvedObjectEvents(limit, objectKind, objectName, objectNamespace)`** — returns up to **`limit`** recent formatted event lines for Kubernetes `Event` objects whose `involvedObject` matches the given kind, name, and namespace. Pass **`""`** for `objectNamespace` when the involved object is not namespaced. Uses cluster-side entities that carry `clusterEntity`; intended for built-in rules and custom rules that need workload-correlated events without extra API calls.

Evaluation uses the CEL **expression** path. **Errors** are handled per [values.md — Tri-state and non-boolean outcomes](values.md#tri-state-and-non-boolean-outcomes).

## Usage in CLI Predicates

The CLI uses CEL predicates for filtering entities during operations:

```bash
# Include only Deployments
hydra gitops dump production --include "kind == 'Deployment'"

# Exclude system namespaces
hydra gitops dump production --exclude "namespace == 'kube-system'"
```

### Predicate Variables

When used for entity selection, the same entity variables are available:

```cel
kind == 'Deployment'
namespace == 'kube-system'
gvk == 'apps/v1/Deployment'
name.startsWith('nginx')
labels().exists(k, k == 'app.kubernetes.io/name')
annotations().getOrEmpty('hydra/skip') != 'true'
```

### Entity inventory and managed namespaces (uninstall / backup / refs / clones)

Namespace-oriented CEL should use **`managedNamespaces()`**, **`templateEntities(...)`**, **`clusterEntities(...)`**, or **`entities(...)`** instead of the removed **`HydraManagedNamespaces`** variable. **`managedNamespaces()`** matches the old sorted deduplicated namespace list. Membership checks that previously used `HydraManagedNamespaces.filter(n, n == ns)` can use **`size(templateEntities({"namespace": ns})) > 0`** (plus the **`gvk == "v1/Namespace"`** / **`templateEntities({"name": name})`** case when evaluating Namespace objects), or filter **`managedNamespaces()`** as before.

#### Uninstall predicate compilation scope

**Uninstall predicate compilation paths** are the `hydra-go` command helpers that compile ref-group predicate strings from `global.hydra.refs` while driving **cluster uninstall selection** (evaluation against live cluster entities, including `clusterEntity` where applicable). The scope is exactly these three predicate sources—do not mix in unrelated CLI filters such as `hydra gitops dump --include`:

1. **`uninstall` and `backup`** — Predicates collected for `MarkAsSelectedByUninstallPredicates`: uninstall predicates plus **backup** predicates from the same marking pass (`HydraAppUninstallPredicates` and `HydraAppBackupPredicates`). Backup participates here because the backup tag implies uninstall behavior for selection.
2. **`uninstall-safe`** — Predicates collected for `MarkAsSelectedBySafeForUninstallationPredicates` (`HydraAppUninstallSafePredicates`). This path may also bind other CEL inputs (for example a `namespaces` set for `ns in namespaces`).
3. **`uninstall-force`** — Predicates collected for uninstall-force separation (`HydraAppUninstallForcePredicates`, evaluated via `SeparateUninstallForceLeftovers` / `separateEntitiesByForcePredicates`).

#### Contract

For **every** path in that scope, compilation must use **`cel.NewEnvWithEntityInventory(rendered)`** (plus any extra options such as **`SetSupport("namespaces", ...)`** on the safe path) with **`rendered`** equal to the **same** selected-app template entity set used to collect predicates for that helper, whenever app-defined predicates may call **`managedNamespaces()`** or otherwise depend on template-derived inventory. Do not rely on implicit ambient state.

This is an implementation-facing API contract in `hydra-go`. It does not change CLI flags, command names, or `global.hydra` YAML shape beyond CEL expression strings in user values.

## Usage in Local Query Projection

`hydra local find` reuses the same CEL environment but evaluates expressions against rendered entities instead of live cluster resources. Because the rendering pipeline enriches these entities with Hydra metadata, projection expressions can access:

- standard entity fields such as `gvk`, `group`, `version`, `kind`, `ns`, and `name`
- rendered resource content via `templateEntity`
- Hydra metadata such as `appIds`, `appNamespace`, `templatePath`, `repoPath`, and `absPath`

Example:

```bash
hydra local find prod.*.* --include 'kind == "KafkaUser"' --pick 'appIds[0]' --uniq
```

The `--pick` expression is required for `hydra local find` and must return a YAML-serializable value:

- scalar values such as strings, booleans, or numbers
- lists such as `appIds`
- maps such as `{"appId": appIds[0], "kind": kind}`

When `--uniq` is set, Hydra deduplicates the projected values after CEL evaluation and before YAML serialization. Deduplication uses the normalized projected value, not the original entity.

## Custom Types

### RefBuilder Type

The `RefBuilder` is a custom CEL type that accumulates reference definitions:

```text
refBuilder()
  │
  ├── .outgoing(id('v1/SA', ns, name))     Add outgoing endpoint
  │     │
  │     ├── .label('serviceAccount')         Set label
  │     │
  │     └── .reverse()                        Mark as reversed
  │
  ├── .incoming(id('v1/SA', ns, subject))   Add incoming endpoint
  │
  └── (returns list of RefDefinitions)
```

The builder collects multiple incoming/outgoing definitions in a single expression. Each `.incoming()` or `.outgoing()` call adds a new `RefDefinition` to the builder's internal list.

### Value Type

Custom CEL type adapter for handling Hydra `Value` interface types in CEL expressions.

## Data Flow

```text
CEL Expression String (from ref-parser YAML or CLI --include flag)
  │
  ▼
cel.NewEnv(extraOptions...)
  │  Create environment with all extensions
  │
  ▼
env.CompileExpression() or env.CompilePredicate()
  │  Parse, check, compile to program
  │
  ▼
Expression.Eval(entity) or Predicate.EvalBool(entity, missingKeys)
  │  Convert entity to map via entityToMap()
  │  Evaluate CEL program
  │  Return result (any value or bool)
  │
  ▼
RefDefinitions (for ref-parsers) or Selected Entities (for predicates)
```
