# hydra gitops template

Render Helm templates for selected applications and print Kubernetes manifests, using the same general workflow as [`hydra local template`](../local/template.md) but with **live API discovery** and **cluster-wide Hydra configuration** for the post-render steps.

## Synopsis

```text
hydra gitops template <appId> [appId...] [flags]
```

## Description

Hydra renders the charts for the apps you name, then runs the same class of post-processing that [`hydra gitops diff`](diff.md) and [`hydra gitops apply`](apply.md) use for **template-side** objects:

1. **Early `global.hydra.templatePatches`** — For each app, Hydra merges that app’s Helm `global.hydra` with the **union** of chart-scoped Hydra ConfigMap `data.hydra` documents discovered from the **full-cluster** template catalog (same idea as `hydraAppMergedValuesMap` for printed values). Hydra applies those rules once before scope validation, so patches can fix invalid raw chart output such as `metadata.namespace` on a cluster-scoped resource.
2. **Scope propagation** — Namespaced vs cluster-scoped resolution uses built-in defaults, live discovery from the cluster, and CRD manifests from a **full-cluster** Helm render (all apps on the cluster, respecting the same skip-root-app rules as other cluster render paths) so types defined in other apps still resolve correctly for the apps you print.
3. **Preferred `apiVersion` normalization** — For each `group/kind`, Hydra rewrites `apiVersion` to the version the apiserver reports as preferred (with a one-time warning per normalized GVK in the log). This matches how [`RenderCluster`](../../../develop/hydra-go/commands.md) prepares manifests before apply/diff.
4. **Final `global.hydra.templatePatches`** — Hydra applies the same resulting rules again to **that app’s** printed manifest set after normalization. For normal chart entities, **declaring-app scoping** is unchanged: rules attributed to app *A* run only when the resource’s primary template `appId` is *A*; rules from global ConfigMap fragments (empty template `appIds`) apply to every resource. **Synthetic `kubernetes-defaults-*` documents** (see below) start **without** template `appIds`; a rule with non-empty `declaringApp` may still run there if its `declaringApp` equals the **resolved namespace owner** (same resolution as clone targets: `global.hydra.ownerNamespaces`, then `v1/Namespace` objects, then sole-app inference from the full-cluster render). If a patch **changes** such a synthetic object, Hydra assigns **one** template `appId` so the object participates in app-scoped output, diff, and review: either the `declaringApp` of the rules that mutated it (all must agree) or, when only **global** rules (`declaringApp` empty) mutated it, the namespace owner. If global rules patch a synthetic object but no owner can be resolved, Hydra errors.

The command is **read-only** for the cluster: it performs discovery (and context validation like other `hydra gitops` commands) but does not apply resources.

Output is sorted multi-document YAML without Helm `# Source:` headers, same style as [`hydra local template`](../local/template.md). Optional [`hydra local template`](../local/template.md)-style **`--bootstrap`** clone appendix is supported: clone-only documents are normalized and patched with the **same** cluster-wide patch pipeline.

## Helm chart input values (shared with `hydra gitops values`)

For each app, the values map passed into `helm template` uses the same pipeline as [`hydra gitops values`](values.md): **`PrepareClusterHelmMergedHydraMaps`** (Helm `global.hydra` merged with Hydra ConfigMap `data.hydra` from the full-cluster catalog, then owner-namespace inference) followed by **`ClusterHelmInputValuesMap`** (replace `global.hydra` in `LoadValuesMap`, including root-only `global.hydra.cluster` injection).

Because that merged map depends on a first Helm render of the selected apps (to partition ConfigMap documents), `hydra gitops template` performs **two** selected-app `helm template` passes: a catalog render to compute merged Hydra maps, then a second render with those maps wired in so chart logic sees the same `global.hydra` as `hydra gitops values` would print.

## Differences from `hydra local template`

| Topic | `hydra local template` | `hydra gitops template` |
| ----- | ------------------------ | ------------------------ |
| Cluster connection | Not required | Required (kubeconfig + validated context) |
| `apiVersion` in YAML | As rendered by Helm / charts | Normalized to apiserver-preferred versions per `group/kind` where discovery provides a preference |
| Source of `templatePatches` rules | Per-app Helm `global.hydra` from chart `LoadValuesMap` (**without** the `PrepareClusterHelmMergedHydraMaps` / second `helm template` replay used by [`hydra gitops values`](../cluster/values.md)), **plus** the **union** of Hydra ConfigMap `data.hydra` fragments from the per-app render **and** the full-cluster **scope-catalog** entities (by id) | Same Hydra ConfigMap merge as the local command, but Helm-side `global.hydra` is first replaced using **`PrepareClusterHelmMergedHydraMaps`** (full-cluster catalog + owner inference) and a **second** selected-app render, so chart logic and Helm-embedded `templatePatches` match [`hydra gitops values`](../cluster/values.md) |
| Synthetic `kubernetes-defaults-*` bundle (`v1/Namespace`, namespaced `ServiceAccount` `default`, `ConfigMap/kube-root-ca.crt`) | Not added | Appended for namespaces **exclusive** to the printed app(s), then deduplicated when the chart already emits the same id ([`WithoutDuplicateSyntheticKubernetesDefaults`](../../../develop/hydra-go/commands.md)); template `appIds` are set **only** when `templatePatches` change that synthetic object (see description above) |

These three object kinds mirror what Kubernetes creates for a new namespace (control-plane defaults): a `Namespace` manifest, the namespaced **`default`** `ServiceAccount`, and the **`kube-root-ca.crt`** `ConfigMap` (Hydra leaves `ca.crt` empty here; the shape exists so diffs/refs line up with a live namespace).

When you are offline or only care about raw chart output, use [`hydra local template`](../local/template.md). When you want manifests that align with **this cluster’s** API preferences and see the effect of **cluster-wide** Hydra ConfigMap fragments on `templatePatches`, use `hydra gitops template`.

## When To Use It

- Inspect YAML that is closer to what server-side normalization and ref indexing expect for **this** cluster.
- Debug `templatePatches` that live in Hydra ConfigMaps owned by **other** apps (rules still respect declaring-app semantics when applied to each app’s resources).
- Compare mentally with [`hydra gitops diff`](diff.md): diff compares the rendered template side to live objects; this command only prints the template side after the cluster-aware pipeline.

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
| `--crd-mode` | | How to treat unknown CRDs: `error` (default) or `ignore` (same meaning as on other cluster commands) |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--bootstrap` | | Same clone-appendix behavior as [`hydra local template`](../local/template.md) |

Parent command `hydra gitops` also exposes `--qps` and `--api-burst` for Kubernetes client throttling (see [Cluster commands](../README.md#cli-groups)).

## Examples

```bash
hydra gitops template prod.infra.cert-manager

hydra gitops template prod.infra.monitoring prod.infra.logging

hydra gitops template prod.** --exclude-app prod.cluster-infra.cert-manager

hydra gitops template prod.apps.my-service --include 'kind == "Deployment"'
```

## See Also

- [`hydra local template`](../local/template.md) — offline render without API normalization, without synthetic `kubernetes-defaults-*` objects, and with `templatePatches` collected from **Git-rendered** charts (scope catalog + per-app render); patch rules are **not** read from the API, but Helm input also **does not** use the merged `global.hydra` replay described for this command
- [`hydra gitops diff`](diff.md) — rendered vs live comparison
- [`hydra gitops apply`](apply.md) — apply rendered manifests
- [`hydra local config`](../local/config.md) — inspect merged `global.hydra` for one app’s render
- [`hydra gitops values`](values.md) — full Helm values with `global.hydra` merged from all apps’ Hydra ConfigMaps (no live diff)
