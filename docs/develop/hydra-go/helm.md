# Helm Chart Processing Architecture

The Helm package handles all aspects of Helm chart processing: loading charts from disk, downloading dependencies, rendering templates, merging values, and splitting manifests into individual YAML documents. It bridges between the Hydra context hierarchy and the entity system.

## Key Concepts

- **Chart loading** — `LoadChart()` with thread-safe `ChartCache` (keyed by `path:networkMode`); supports persistent and temporary chart directories
- **Dependency management** — `DownloadChartDependencies()` with online/offline/local network modes; recursive transitive dependency resolution
- **Values processing** — `LoadValuesMap()` merges chart defaults with input values, processes dependencies, extracts Hydra fallback values from `infra_library`, runs `ToRenderValues` and YQ cleanup (fully merged values for inspection). **`CoalescedValuesMapBeforeRender()`** stops before `ToRenderValues` and matches the **single** coalesce round Helm expects for `helm.Template` / `Install.Run`. Child/root **template** paths use raw install-style values (`MergedChildValuesForHelmInstall`, `ClusterHelmInstallValuesMap` for cluster template); **`ClusterHelmInputValuesMap`** stays on `LoadValuesMap` for `hydra gitops values` display
- **Template rendering** — `Template()` uses Helm `action.Install` with `DryRunClient` strategy; includes CRDs and hooks
- **Persistent Helm template cache** — For each root app in the GitOps tree, Hydra may store rendered manifests under `<rootAppDir>/.hydra/cache/helm/`. Root apps use `cache.yaml` + `templates.yaml`; child apps use `cache-<childApp>.yaml` + `templates-<childApp>.yaml` in the same root app directory. The `cache*.yaml` file holds a serialized snapshot of the effective render inputs (including merged values, release name, namespace, Kubernetes version, `skipCrds`, and Helm network mode). If the on-disk snapshot matches the current inputs, Hydra reuses `templates*.yaml` instead of running Helm again. Disable with `--no-cache` or `HYDRA_NO_CACHE` (also disables in-process values/template/chart caches for that invocation).
- **Manifest splitting** — `SplitManifestMap()` splits by `---` separator, extracts `# Source:` paths into `map[templatePath][]YamlString`
- **ArgoCD integration** — `YqPatchArgo()` adds tracking annotations in format `{appId}:{group}/{kind}:{namespace}/{name}`

## Source Files

`core/helm/render.go`, `core/helm/values.go`, `core/helm/manifest.go`, `core/helm/chart_cache.go`, `core/helm/chartdirectory.go`, `core/helm/clone.go`, `core/helm/downloader.go`, `core/helm/hydra_fallback_values.go`

→ **Full details:** [details/helm.md](details/helm.md)
