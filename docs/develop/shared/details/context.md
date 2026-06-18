# Hydra Context Architecture

## Overview

The Hydra context represents the hierarchical structure of a GitOps repository. It defines how clusters, root applications, and child applications are organized on disk and how values are loaded and merged at each level.

**Source files:** `core/hydra/context.go`, `core/hydra/cluster.go`, `core/hydra/root_app.go`, `core/hydra/child_app.go`, `core/hydra/hydra.go`

## Type Hierarchy

```text
Hydra (interface)
  │
  ├── Context (base)
  │     Directory containing clusters
  │
  ├── Cluster (extends Context)
  │     Specific cluster within a context
  │
  ├── HydraApp (interface, extends Hydra)
  │     │  Adds: AppId(), Namespace(), Template()
  │     │
  │     ├── RootApp (extends Cluster)
  │     │     Root Helm application (ArgoCD Application)
  │     │
  │     └── ChildApp (extends RootApp)
  │           Child Helm application within a root app
  │
  └── HydraValues
        Configuration extracted from values (kubectl contexts, predicates, etc.)
```text

### Interfaces

```go
// Hydra is the base interface for all levels
type Hydra interface {
    L() log.Logger
    AsContext() *Context
    AsCluster() *Cluster
    AsRootApp() *RootApp
    AsChildApp() *ChildApp
    AsApp() HydraApp
    Config() types.Config
    WithCluster(cluster types.ClusterName) (*Cluster, error)
    WithApp(app types.AppId) (HydraApp, error)
    LoadValuesMap(mode types.NetworkMode) (types.ValuesMap, error)
    Description() string
}

// HydraApp extends Hydra for application levels (RootApp, ChildApp)
type HydraApp interface {
    Hydra
    AppId() types.AppId
    Namespace(types.NetworkMode) (types.Namespace, error)
    Template(types.NetworkMode, types.KubernetesVersionOrFallback) (types.YamlString, error)
}
```

## Directory Structure

A Hydra context follows this directory layout:

```text
<context>/                                Context root
├── values.yaml                           Context-level values
├── in-cluster/                           Cluster "in-cluster" (management cluster)
│   ├── values.yaml                       Cluster-level values
│   ├── argocd/                           Root application "argocd"
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   └── templates/
│   └── cluster-infra/                    Root application "cluster-infra"
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
├── <cluster>/                            Additional cluster (e.g. "dev", "prod")
│   ├── values.yaml                       Cluster-level values
│   └── <root-app>/                       Root application
│       ├── Chart.yaml                    Helm chart definition
│       ├── values.yaml                   Root app values
│       ├── templates/                    ArgoCD Application templates
│       └── charts/                       Dependencies (child apps)
│           └── <child-app>/              Child application chart
│               ├── Chart.yaml
│               ├── values.yaml
│               └── templates/
└── group-<name>/                         Optional value group directories
    └── values.yaml
```text

**Key structural points:**

- Each top-level directory (except files and group directories) is a **cluster**.
- `in-cluster` is the management cluster where ArgoCD runs.
- Subdirectories within a cluster directory are **root applications**, not clusters.
- Root apps generate ArgoCD Application CRs for their child apps.

## Path Resolution

### `GetClusters()` → []\*Cluster

Returns all clusters in the context by listing top-level directories in the context path. Each directory is treated as a cluster (e.g. `in-cluster`, `dev`, `prod`).

```go
func (c *Context) GetClusters() ([]*Cluster, error)
```

### `ResolvePath(path)` → Hydra

Analyzes a filesystem path to determine which Hydra level it represents:

```text
Input path                              Result type
──────────                              ───────────
/repo/gitops/                           Context
/repo/gitops/dev/                       Cluster "dev"
/repo/gitops/dev/demo/                   RootApp "dev.demo"
```text

The resolution algorithm searches upward from the given path for the `in-cluster/argocd/` directory structure:

1. Walk up the directory tree to find the context root (contains `in-cluster/argocd/`)
2. If path extends into `<cluster>/` → Cluster
3. If path extends further into `<cluster>/<root-app>/` → RootApp
4. Otherwise → Context

### `ResolvePathWithAppId(path, appId)` → HydraApp

Resolves a path and validates it against a specific AppId:

```text
AppId format: cluster.rootApp          → RootApp
              cluster.rootApp.childApp → ChildApp
```

The function resolves the path to a Cluster, then navigates to the specified app:

1. Resolve path to Cluster
2. Extract `rootApp` from AppId
3. If AppId has `childApp` part → create ChildApp
4. Otherwise → create RootApp
5. Validate that the resolved Cluster matches the AppId's cluster

### `ResolvePathWithCluster(path, clusterName)` → Cluster

Resolves a path and validates it against a specific cluster name.

## Values Loading

Values are loaded bottom-up through the hierarchy, with each level adding its own values on top. The merge strategy is deep-merge (nested maps are merged recursively; non-map values are overwritten).

### Values Loading Order

```text
Level          Source files (in merge order)
─────          ───────────────────────────────
Context        group-*/values.yaml, <context>/values.yaml
Cluster        Context values + in-cluster/values.yaml + <cluster>/values.yaml
RootApp        Cluster values + <root-app>/values.yaml
ChildApp       Extracted from RootApp values + child-specific overrides
```text

### Context Values

```go
func (c *Context) LoadValuesMap(mode types.NetworkMode) (ValuesMap, error)
```

1. Load group-level values from `group-*/values.yaml` directories
2. Load context-level values from `<context>/values.yaml`
3. Deep-merge: groups first, then context overrides

### Cluster Values

```go
func (c *Cluster) LoadValuesMap(mode types.NetworkMode) (ValuesMap, error)
```text

1. Load context values (parent)
2. Load shared cluster values from `in-cluster/values.yaml`
3. Load cluster-specific values from `<cluster>/values.yaml`
4. Deep-merge in order

### RootApp Values

```go
func (r *RootApp) LoadValuesMap(mode types.NetworkMode) (ValuesMap, error)
```

1. Load cluster values (parent)
2. Load root app values from `<root-app>/values.yaml`
3. Deep-merge

### ChildApp Values

```go
func (c *ChildApp) LoadValuesFromRootApp() (ValuesMap, error)
```text

1. Load root app values
2. Extract child-app-specific values from the root app configuration
3. Extract global values
4. Merge: global values + child-specific overrides
5. Apply extra value files if configured

## Template Rendering

### RootApp Template

```go
func (r *RootApp) Template(mode types.NetworkMode, k8sVersion types.KubernetesVersionOrFallback) (YamlString, error)
```

Renders the root app chart (typically producing ArgoCD Application manifests):

1. Load chart from `RootAppPath()` via `helm.LoadChart()`
2. Load values via `LoadValuesMap()`
3. Inject `global.hydra.cluster` with the actual cluster name (so templates like `infra_library.fn.hydra.cluster` resolve correctly instead of falling back to `in-cluster`)
4. Process values via `helm.LoadValuesMap()` (merges chart defaults, processes dependencies)
5. Render via `helm.Template()`
6. Apply ArgoCD tracking-id annotations via `yq.YqPatchArgo()`

### ChildApp Template

```go
func (c *ChildApp) Template(mode types.NetworkMode, k8sVersion types.KubernetesVersionOrFallback) (YamlString, error)
```text

Renders the child app chart (producing Kubernetes resource manifests):

1. Resolve child app chart path from root app dependency structure
2. Load chart via `helm.LoadChart()`
3. Load values via `LoadValuesFromRootApp()`
4. Process values via `helm.LoadValuesMap()`
5. Render via `helm.Template()`
6. Apply ArgoCD tracking-id annotations
7. Append static manifests (if any exist in `static/` directory)

## AppId

The `AppId` type uniquely identifies an application in the Hydra hierarchy:

```text
Format: cluster.rootApp              (root application)
        cluster.rootApp.childApp     (child application)

Examples:
  production.argocd                  Root app "argocd" in cluster "production"
  production.monitoring.prometheus   Child app "prometheus" under root app "monitoring"
```

```go
type AppId string
```text

AppId is a string type. Components are extracted via dot-separated parsing methods:

**Methods:**

| Method                                                  | Description                                        |
| ------------------------------------------------------- | -------------------------------------------------- |
| `NewAppId(string)`                                      | Parses and validates dot-separated string to AppId |
| `NewRootAppId(ClusterName, RootAppName)`                | Creates a root app AppId                           |
| `NewChildAppId(ClusterName, RootAppName, ChildAppName)` | Creates a child app AppId                          |
| `IsRootApp()`                                           | Returns true if AppId has 2 parts (no child app)   |
| `ClusterName()`                                         | Extracts cluster name from AppId                   |
| `RootAppName()`                                         | Extracts root app name from AppId                  |
| `ChildAppName()`                                        | Extracts child app name (nil for root apps)        |

## Caching

All expensive operations are cached to avoid redundant computation:

```text
hydraCaches
├── chartCache         Chart loading: path+networkMode → *chart.Chart
├── valuesCache        Values loading: AppId+NetworkMode → ValuesMap
└── templateCache      Template rendering: AppId+NetworkMode+K8sVersion → YamlString
```

**Cache properties:**

- Thread-safe (uses `sync.RWMutex`)
- Lazy-loaded (computed on first access)
- Online mode results are also cached for offline mode
- Errors are cached to prevent repeated failing operations

## Kubernetes Context Validation

Before performing cluster operations, Hydra validates the current kubectl context:

```go
func ValidateCurrentContext(hydraValues HydraValues) error
```text

1. Read current kubectl context from `~/.kube/config`
2. Check against `HydraValues.KubectlContexts` (list of allowed contexts)
3. Validate context name, cluster name, and auth info match
4. Reject if no matching context is configured

This prevents accidental operations against wrong clusters.

## HydraValues Configuration

`HydraValues` is extracted from the merged values at any level and provides Hydra-specific configuration:

```go
type HydraValues struct {
    KubectlContexts    []HydraKubectlContext          // Allowed kubectl contexts
    Uninstall          map[string]HydraUninstallGroup // Named groups with CEL predicates for uninstall (mode: "uninstall"|"safe"|"force")
    UninstallFinalizer []string                       // Finalizer names to remove cluster-wide
    KubernetesVersion  KubernetesVersion              // Target K8s version
    ValuesCleanup      string                         // YQ expression for values cleanup
}

type HydraKubectlContext struct {
    Name     string  // kubectl context name
    Cluster  string  // kubectl cluster name
    AuthInfo string  // kubectl auth info name
}
```

## Data Flow

```text
Path (CLI argument)
  │
  ▼
ResolvePath() / ResolvePathWithAppId()
  │  Walks directory tree, determines Hydra level
  │
  ▼
Context / Cluster / RootApp / ChildApp
  │
  ├── LoadValuesMap()
  │     Hierarchical merge: context → cluster → app
  │
  ├── HydraValues()
  │     Extract Hydra configuration from values
  │
  ├── ValidateCurrentContext()
  │     Ensure kubectl points to correct cluster
  │
  └── Template()
        Render Helm chart with merged values
        Apply ArgoCD annotations
        │
        ▼
      YamlString (rendered manifest)
```text
