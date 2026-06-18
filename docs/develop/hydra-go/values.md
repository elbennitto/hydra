# Values Pipeline Architecture

The values pipeline handles loading, merging, and processing of Helm values throughout the Hydra context hierarchy. Values are loaded from multiple YAML files at different levels (context, cluster, root app, child app) and deep-merged to produce the final values for chart rendering.

## Key Concepts

- **Values hierarchy** — Context → Cluster → RootApp → ChildApp, each level adding its own overrides (right-side precedence)
- **Deep merge** — Maps merged recursively, non-maps replaced, nil/missing uses the other side
- **Core functions** — `LoadValuesFile` (single file), `LoadAndMergeValuesFile` (load + merge), `MergeValues` (deep merge), `Lookup` (nil-safe traversal)
- **Helm values processing** — `LoadValuesMap()` merges chart defaults, processes dependencies, extracts Hydra fallback values, applies YQ cleanup, merges globals (includes `ToRenderValues`). Template rendering must pass **install-style** values into `helm.Template` (see `CoalescedValuesMapBeforeRender`, `ClusterHelmInstallValuesMap` vs `ClusterHelmInputValuesMap` in [details/helm.md](details/helm.md))
- **HydraValues extraction** — `HydraValues()` extracts Hydra config from `global.hydra` key (kubernetesVersion, refs, uninstall-finalizer, scale, diff, templatePatches, clones, ready, …). **`templatePatches`** is validated in `HydraValues.Validate()` for Helm-loaded values; fragments merged only from ConfigMap `data.hydra` are validated when rules are collected (`templatePatchEntriesFromMergedMap`). At cluster-template/apply/diff time, effective patch rules also use the **union of chart-scoped** Hydra ConfigMaps across apps (see `HydraTemplatePatchRuleEntries` in `core/hydra/template_patch_merge.go`), analogous to `hydraAppMergedValuesMap` for merged `global.hydra` maps
- **Scale workload definitions** — `global.hydra.scale` declares custom CRD-based resources (e.g., Strimzi Kafka CRs) as scale targets with GVK and replica paths
- **Child app values** — Extracted from root app's rendered values; global values as base, child-specific overrides on top
- **Value files export** — Cluster dump collects value files from all hierarchy levels and writes to `values/` directory

## Source Files

`core/values/values.go`, `core/helm/values.go`, `core/hydra/hydra_values.go`

→ **Full details:** [details/values.md](details/values.md)
