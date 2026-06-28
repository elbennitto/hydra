# hydra local template

Render Helm templates for one or more applications and output the resulting Kubernetes manifests.

## Synopsis

```text
hydra local template <appId> [appId...] [flags]
```

## Description

Processes the Helm charts for the specified applications through the Hydra rendering pipeline and outputs the resulting Kubernetes YAML manifests. Runs entirely locally — no cluster connection required.

This is the go-to command for inspecting what Hydra would produce before deploying. Conceptually equivalent to `helm template`, but uses Hydra's embedded Helm engine with its context hierarchy, values composition, and dependency resolution applied.

Hydra may cache Helm render results under each root app directory in GitOps: `.hydra/cache/helm/cache.yaml` and `templates.yaml` for the root app chart, and `cache-<childApp>.yaml` / `templates-<childApp>.yaml` for child apps. Cached output is reused only when the serialized render inputs match. Use `--no-cache` or set `HYDRA_NO_CACHE` to force a full render and skip these caches.

Output is always normalized to sorted multi-document YAML from Hydra’s entity pipeline (no Helm `# Source:` file headers). **`hydra local template` does not append** the synthetic `kubernetes-defaults-*` bundle (`v1/Namespace`, namespaced `ServiceAccount` named `default`, `ConfigMap/kube-root-ca.crt`) that `hydra gitops template` and [`RenderCluster`](../../../develop/hydra-go/commands.md) add for exclusive namespaces; those placeholders exist only on **cluster** render paths.

Optional **`global.hydra.templateFiles`** mutates loaded chart files before Helm renders anything. Use it to move a local `templates/...` file onto a dependency path such as `charts/upstream/templates/...`, or to delete matching template files by regex before render. See [`templateFiles in Values`](../../appendix/values/template-files.md).

The same `templateFiles` preprocessing also applies when you package the chart with [`hydra local package`](package.md) or when CI runs `hydra ci run publish`.

Optional **`global.hydra.templatePatches`** in the app’s values (or Hydra ConfigMap `data.hydra`) runs before scope validation and again as a final step on the printed manifest set (per-app render and, when present, the clone appendix after `---`). Rules can also live **only** in another app’s chart (for example a cluster-wide `ConfigMap` under the Argo CD app): `hydra local template` still merges those fragments by combining the **full-cluster scope-catalog** render (from the Git checkout) with the selected app’s partition when collecting patch rules (see [`templatePatches in Values`](../../appendix/values/template-patches.md)). The early pass can fix invalid raw chart output such as `metadata.namespace` on a cluster-scoped resource; the final pass must not change resource identity (`apiVersion`, `kind`, `metadata.name`, `metadata.namespace`) and must not mutate Hydra configuration ConfigMaps (`hydra-gitops.org/hydra-config: "true"` with `data.hydra`).

When `--include` or `--exclude` is set, template patches run **first**, then only resources matching the combined CEL predicates are printed (same CEL model as [`hydra local find`](find.md) for the filter step).

To print only the sorted, deduplicated Hydra resource ids for the same render (for scripting or quick inventory), use [`hydra local list`](list.md).

## When To Use It

Use `hydra local template` when you want to answer local rendering questions without touching a cluster:

- What YAML will Hydra generate for this app?
- Did my values or chart change alter the rendered output?
- Can another tool validate the rendered manifests?

When you need live-cluster comparison instead of local rendering, continue with [`hydra gitops diff`](../cluster/diff.md).

When you want **rendered** YAML that still reflects **this cluster’s** preferred API versions, includes **synthetic kubernetes-defaults** objects for exclusive namespaces, and applies **`templatePatches`** with the same **namespace-owner** attribution rules used on other cluster commands, use [`hydra gitops template`](../cluster/template.md). That command is read-only but requires kube API access for discovery; see its manual page for a feature comparison with `hydra local template`.

## Arguments

| Argument | Description                                                                                |
| -------- | ------------------------------------------------------------------------------------------ |
| `appId`  | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--bootstrap` | | Include `global.hydra.clones` rules tagged `bootstrap` and append their YAML after each app’s render; fails if the flag is unnecessary (debugging aid) |

## Examples

```bash
# Render templates for a single app
hydra local template prod.infra.cert-manager

# Render all child apps under a root app
hydra local template prod.infra.*

# Render only Deployments
hydra local template prod.apps.my-service --include 'kind == "Deployment"'

# Pipe to kubectl for validation
hydra local template prod.infra.cert-manager | kubectl apply --dry-run=server -f -

# Compare rendered output between environments
diff <(hydra local template staging.infra.cert-manager) <(hydra local template prod.infra.cert-manager)

# Pipe to kubeconform or similar tools
hydra local template prod.infra.* | kubeconform -strict

# Render offline (no chart downloads)
hydra local template prod.infra.* --helm-network-mode offline
```

## Example: Argo CD server-side apply on CRDs

Prefer a **single** Hydra ConfigMap in the **Argo CD** app chart (`hydra-gitops.org/hydra-config: "true"`, `data.hydra.templatePatches`) so every cluster app inherits the same rule without repeating YAML in each chart’s values. Alternatively, declare the rule under `global.hydra.templatePatches` in a given app’s Helm values. Use a CEL predicate on `CustomResourceDefinition` and a yq expression that sets `metadata.annotations["argocd.argoproj.io/sync-options"]`. Keep the expression idempotent if you merge with existing options (comma-separated list). Exact yq depends on your charts; validate with `hydra local template` before committing.

```yaml
global:
  hydra:
    templatePatches:
      crdArgoServerSideApply:
        predicate: 'gvk == "apiextensions.k8s.io/v1/CustomResourceDefinition"'
        patches:
          - yq: '.metadata.annotations."argocd.argoproj.io/sync-options" = "ServerSideApply=true"'
```

## Tutorials

- [Adding Manifests to the Helm Charts](../../tutorials/introduction/02-00-adding-manifests-to-the-helm-charts.md) — uses `hydra local template` to render the first app manifests and compare Hydra output with Helm

## See Also

- [`hydra local source`](source.md) — print unrendered chart template files from disk only (no render; use `--include-path` for path prefixes)
- [`hydra local package`](package.md) — build the app chart as a Helm `.tgz` archive using the same packaging path as CI publish
- [`hydra local values`](values.md) — inspect the computed values that feed into template rendering
- [`hydra local config`](config.md) — inspect Helm `global.hydra` and Hydra ConfigMaps
- [`hydra gitops template`](../cluster/template.md) — same style of sorted YAML with live API discovery, preferred `apiVersion` normalization, merged Helm `global.hydra` (as in [`hydra gitops values`](../cluster/values.md)), synthetic `kubernetes-defaults-*` for exclusive namespaces, and `templatePatches` including namespace-owner attribution for synthetic objects
- [`hydra gitops diff`](../cluster/diff.md) — compare rendered output against live cluster state
- [`hydra gitops apply`](../cluster/apply.md) — render and apply in one step
