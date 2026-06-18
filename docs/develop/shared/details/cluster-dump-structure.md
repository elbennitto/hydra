# Cluster Dump Directory Structure

## Overview

The `hydra ui <dir>` command exports the full dependency model, rendered manifests, and Helm chart archives for **all clusters** of the current context into a directory. The context is resolved via the `--hydra-context` flag. The Hydra UI can then serve this directory to display manifests and Go templates alongside the dependency graph.

The output directory contains one subdirectory per cluster. Each cluster subdirectory follows the same per-cluster structure described below.

## Directory Layout

```tree
<dir>/
    in-cluster/
        hydra.yaml
        charts/
            <chart-name>.tgz
        manifests/
            <app-id>/
                <group>/<version>/<kind>/<namespace>/<name>.yaml
        values/
            files/
            fallback/
            merged/
    dev/
        hydra.yaml
        charts/
        manifests/
        values/
    prod/
        hydra.yaml
        charts/
        manifests/
        values/
```

Each cluster subdirectory has the same internal structure:

```tree
<dir>/<cluster>/
    hydra.yaml                                    # Dependency model (entities, groups, references, charts, valueFiles, appValues)
    charts/
        <chart-name>.tgz                          # Helm chart archive (one per unique chart name)
        ...
    manifests/
        <app-id>/                                 # First appId of the entity
            <group>/                              # API group, "(core)" for core API
                <version>/                        # API version (e.g. "v1")
                    <kind>/                       # Resource kind (e.g. "Deployment")
                        <namespace>/              # Namespace, "(cluster)" for cluster-scoped
                            <name>.yaml           # Rendered manifest YAML
    values/
        files/                                    # Values files from the GitOps repository (not in .tgz)
            values.yaml                           # Group-level values
            <context>/
                values.yaml                       # Context-level values
            <context>/<cluster>/
                values.yaml                       # Cluster-level values
        fallback/
            <app-id>.yaml                         # Hydra fallback values per app (from infra_library)
            ...
        merged/
            <app-id>.yaml                         # Fully merged values per app
            ...
```

## Context Export

The command resolves the hydra context via the `--hydra-context` flag and discovers all clusters:

1. `context.GetClusters()` returns clusters discovered from subdirectories of `<context>/in-cluster/` (e.g. `dev`, `prod`). This does **not** include `in-cluster` itself.
2. `in-cluster` is explicitly added via `context.WithCluster(types.InCluster)`.

For each cluster, the command renders apps and writes the output to `<dir>/<cluster>/`:

- **Non-in-cluster clusters** (dev, prod, etc.): A **single-pass render with dual split** is performed:
  1. Get ALL appIds (root + child) via `cluster.AppIds()`.
  2. Call `RenderCluster(cluster, allAppIds, skipRootApps=false)` — renders all apps in one pass.
  3. Split the rendered entities into two groups:
     - **Root-app entities** (where the entity's first appId satisfies `IsRootApp()`) → collected for in-cluster merge.
     - **Child-app entities** (the rest) → exported to `<dir>/<cluster>/`.
  4. For charts/values/etc., call `CollectChartInfo`, `CollectChartObjects`, `CollectValueFiles`, `CollectAppValues`, `CollectFallbackValues` separately for rootAppIds and childAppIds.

  This is more efficient than two passes (each app rendered only once).

- **in-cluster**: Own apps (both root and child) are rendered. Before writing, merged root-app data from all other clusters is added. The combined result is written to `<dir>/in-cluster/`.

**Source:** `cli/action/cluster_view.go::ClusterViewContext`

### CRD-Scope Resolution

CRD-scope resolution (`ApplyScopeInfoMaps` in `render.go`) happens during `RenderCluster` on the full entity set **before** the split into root-app and child-app groups. This ensures cross-app CRD definitions are correctly applied: CRDs from one app inform the scope of entities in other apps within the same cluster.

### CLI / Root Command Changes

| File      | Change                                                                                                                                                                                                  |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ui.go`   | `Use` changes to `"ui <output-dir>"`, `Args` to `cobra.ExactArgs(1)`, `RunE` uses `args[0]` for OutputDir.                                                                                              |
| `root.go` | `RootCommandParams.UICluster` type changes from `func(flags action.ClusterViewClusterFlags) (hydra.Hydra, error)` to `func(flags action.ClusterViewContextFlags) error` (no Hydra return value needed). |
| —         | The `ClusterFlag` is no longer used by the UI command.                                                                                                                                                  |

### Action Layer Structures

| Structure / Function                                                                            | Purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ClusterViewContextFlags`                                                                       | Replaces `ClusterViewClusterFlags`. Contains `OutputDir` but no `ClusterFlag` (all clusters are exported).                                                                                                                                                                                                                                                                                                                                                          |
| `ClusterViewContext(f ClusterViewContextFlags) error`                                           | New entry point. Iterates all clusters, orchestrates render and merge, writes output.                                                                                                                                                                                                                                                                                                                                                                               |
| `clusterRenderResult`                                                                           | Holds render output: entities, chartModels, chartObjects, manifests, valueFiles, appValues, fallbackValues. Also includes `contextParentPath` (string) and git metadata (`gitRemote`, `gitRepoPrefix`, `gitBranch`) needed by `writeClusterExport` to produce a complete `hydra.yaml`. These fields are derived from the cluster's context path and are the same for all clusters in a context, but each `clusterRenderResult` carries them for self-containedness. |
| `renderClusterApps(f ClusterViewFlags, cluster *Cluster, appIds) (*clusterRenderResult, error)` | Extracted render phase. Always renders all apps (`skipRootApps=false`); returns full result. The split into root-app vs child-app entities happens afterward.                                                                                                                                                                                                                                                                                                       |
| `mergeRenderResults(base, extra *clusterRenderResult)`                                          | Merges an extra render result (root app data) into a base result (in-cluster data).                                                                                                                                                                                                                                                                                                                                                                                 |
| `writeClusterExport(f ClusterViewFlags, data *clusterRenderResult, outputDir string) error`     | Write phase. Takes a render result and writes hydra.yaml, charts/, manifests/, and values/ to the given directory.                                                                                                                                                                                                                                                                                                                                                  |

## Root App Merge for In-cluster

Root apps live in `<context>/<cluster>/` (e.g. `<context>/dev/demo/`). When rendered, a root app produces **Application CRs** in the `argocd` namespace. These Application CRs belong to the **management cluster** (`in-cluster`) because the ArgoCD Application CRD only exists there.

Data flow:

1. For each non-in-cluster cluster, `RenderCluster` is called once with all appIds. The full render result is split: root-app entities (and their charts, manifests, values) are collected for merge; child-app entities are written to `<dir>/<cluster>/`.
2. The root-app portion of each `clusterRenderResult` — entities, charts, manifests, and values — is collected.
3. After each non-in-cluster cluster has been rendered and split, each root-app result is merged into the in-cluster render result via `mergeRenderResults(inClusterResult, rootAppResult)`.
4. The combined in-cluster result (own apps + merged root-app data from all other clusters) is written to `<dir>/in-cluster/`.

This ensures that Application CRs originating from root apps across all clusters appear in the in-cluster export, matching where ArgoCD actually manages them.

## Path Conventions

### API Group

| Kubernetes API Group        | Directory Name              |
| --------------------------- | --------------------------- |
| `apps`                      | `apps`                      |
| `rbac.authorization.k8s.io` | `rbac.authorization.k8s.io` |
| _(empty — core API)_        | `(core)`                    |

### Namespace

| Kubernetes Namespace      | Directory Name |
| ------------------------- | -------------- |
| `default`                 | `default`      |
| `kube-system`             | `kube-system`  |
| _(empty — cluster scope)_ | `(cluster)`    |

### App ID

The first `appId` from the entity's `appIds` list is used. Format: `<cluster>.<rootApp>` or `<cluster>.<rootApp>.<childApp>`.

## Example

Given a context with two clusters — `in-cluster` (management cluster) and `dev` — where `dev` has root app `dev.demo` with child app `dev.demo.service-auth`:

```tree
output/
    in-cluster/
        hydra.yaml
        charts/
            demo.tgz
        manifests/
            in-cluster.core-infra.cert-manager/
                apps/
                    v1/
                        Deployment/
                            cert-manager/
                                cert-manager.yaml
            dev.demo/
                argoproj.io/
                    v1alpha1/
                        Application/
                            argocd/
                                dev.demo.service-auth.yaml
        values/
            files/
                ...
            fallback/
                ...
            merged/
                ...
    dev/
        hydra.yaml
        charts/
            service-auth.tgz
        manifests/
            dev.demo.service-auth/
                apps/
                    v1/
                        Deployment/
                            service-auth/
                                service-auth.yaml
                (core)/
                    v1/
                        Service/
                            service-auth/
                                service-auth.yaml
        values/
            files/
                ...
            fallback/
                ...
            merged/
                ...
```

In this example:

- **`output/in-cluster/`** contains the in-cluster's own apps (e.g. `in-cluster.core-infra.cert-manager`) **plus** the Application CR from the dev root app (`dev.demo`). The Application CR `dev.demo.service-auth.yaml` lives in the `argocd` namespace under the `argoproj.io/v1alpha1/Application` path because ArgoCD manages it on the management cluster.
- **`output/dev/`** contains only child app workloads (e.g. `dev.demo.service-auth` Deployment and Service). Root app output is not written here — it was merged into in-cluster.

## Manifest Path in hydra.yaml

Each entity in `hydra.yaml` includes a `manifestPath` field that contains the relative path from the `manifests/` directory to the entity's rendered manifest file. This path matches the file system layout exactly.

```yaml
entities:
  - id: apps/v1/Deployment/cert-manager/cert-manager
    appIds:
      - in-cluster.core-infra.cert-manager
    manifestPath: in-cluster.core-infra.cert-manager/apps/v1/Deployment/cert-manager/cert-manager.yaml
    templatePath: cert-manager/templates/deployment.yaml
    templateIndex: 1
```text

Entities without a rendered manifest (e.g. those tagged with `app:missing`) have no `manifestPath` field.

## Chart Archives

Each unique chart name produces one `.tgz` file in `charts/`. The archive is a standard Helm chart package (gzipped tar) containing the full chart directory with `Chart.yaml`, `values.yaml`, `templates/`, etc.

The entity's `templatePath` field (e.g. `cert-manager/templates/deployment.yaml`) can be used to locate the Go template source file inside the archive. The first path segment matches the chart name.

## Value Files

The `values/` directory contains values files from the GitOps repository that are **not** bundled inside chart `.tgz` archives. These are the hierarchical values files that hydra-go reads during the values merge pipeline.

### Value File Types

| Type      | Description                                              |
| --------- | -------------------------------------------------------- |
| `group`   | Values from the parent directory of the context          |
| `context` | Values from the context directory                        |
| `cluster` | Values from the cluster directory                        |
| `app`     | Extra value files for child apps (on disk, not in chart) |

### hydra.yaml Metadata

```yaml
valueFiles:
  - path: values.yaml
    type: group
  - path: dev/values.yaml
    type: context
  - path: dev/my-cluster/values.yaml
    type: cluster
  - path: dev/my-cluster/my-app/extra-values.yaml
    type: app
    appId: my-cluster.my-app.child

appValues:
  - appId: my-cluster.my-app
  - appId: my-cluster.my-app.child
```

### Fallback Values

The `values/fallback/` directory contains the Hydra fallback values per app as `<appId>.yaml`. These are extracted from the `infra_library` chart dependency (see HELM.md) and represent the default values that are merged as a base before the values hierarchy is applied.

In practice, fallback files are often present only for root app IDs (`<cluster>.<rootApp>`). Reason: fallback extraction runs from the chart dependency graph where `infra_library` is typically configured on root app charts. Child apps usually inherit their effective defaults from root app values plus child chart defaults, so they do not necessarily produce a separate `<cluster>.<rootApp>.<childApp>.yaml` fallback file.

**Source:** `core/export/export.go`

- `WriteFallbackValues(l, dir, fallbackValues)` — Writes fallback values as `<dir>/values/fallback/<appId>.yaml`.

### Merged Values

The `values/merged/` directory contains the fully merged values for each app as `<appId>.yaml`. These represent the final values after all levels of the values hierarchy have been deep-merged.

## Directory Validation

When running the command, validation happens at two levels:

### Top-level output directory (`<dir>`)

1. If `<dir>` does not exist: the parent directory must exist. The command creates `<dir>`.
2. If `<dir>` exists: it must be a directory.

### Per-cluster subdirectory (`<dir>/<cluster>/`)

1. If `<dir>/<cluster>/` does not exist: it is created by the command.
2. If `<dir>/<cluster>/` exists and is a directory containing `hydra.yaml`, `manifests/`, `charts/`, and `values/`: the directory is cleared and re-populated.
3. Otherwise: the command fails with an error.

## Test Plan

| Test                                      | Description                                                                                                                  |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Unit test: `mergeRenderResults`           | Verifies entities, charts, manifests, and values are correctly merged when combining root-app data into in-cluster result.   |
| Unit test: entity split logic             | Verifies entities are correctly partitioned into root-app and child-app groups by `IsRootApp()` on the entity's first appId. |
| Integration test: full context export     | Verifies directory structure `<dir>/<cluster>/` with hydra.yaml, charts/, manifests/, values/ per cluster.                   |
| Edge case: context with only `in-cluster` | No other clusters — should export just in-cluster.                                                                           |
| Edge case: cluster with no root apps      | Should export only child apps, nothing merged to in-cluster.                                                                 |
| Golden file tests                         | Follow project convention: `.given.yaml` / `.expected.yaml` for deterministic output comparison.                             |
