# hydra gitops diff

Show differences between rendered Helm templates and the current cluster state.

## Synopsis

```text
hydra gitops diff <appId> [appId...] [flags]
```

## Description

Renders the selected applications and compares the desired manifests against the live cluster state. The output shows what would change if [`hydra gitops apply`](apply.md) were run with the same selector and filters.

For the live side, Hydra now reads cluster objects from the shared resource model: template rendering and live inventory still originate from different build steps, but the diff reader consumes one normalized per-ID inventory view instead of rebuilding its own live-only slice.

This command is the **diff** and review surface: it prints unified diff output. [`hydra gitops apply`](apply.md) applies manifests and does not emit that diff; it may share internal server-side dry-run building blocks but remains a separate user-facing workflow.

Optional [`-A`](#flags) / [`-B`](#flags) / [`-C`](#flags) flags follow the same idea as **grep**: they control how many **unchanged** lines appear around each change in the unified diff. The underlying differ uses one symmetric context size; if you set more than one of these flags, Hydra uses the **maximum** of the values you gave (so combined `-B` and `-A` behave like grep when you mix context options).

[`--include`](#flags) and [`--exclude`](#flags) narrow **which Kubernetes resources** appear in the diff. They use the same [CEL resource filter](../README.md#cel-resource-filters) rules as [`hydra gitops list`](list.md) and [`hydra gitops dump`](dump.md). Filtering runs **after** the template/cluster merge and orphan detection, immediately before unified diff lines are printed. In the CEL environment, `templateEntity` and `clusterEntity` refer to the unstructured object for each side when that side exists (for example, new resources may only have `templateEntity`; cluster-only orphans only `clusterEntity`).

**Template patches (`global.hydra.templatePatches`)** — After `RenderCluster` and **before** diff-ignore normalization and (in server mode) server-side apply dry-run, Hydra applies optional post-render yq rules from merged Helm `global.hydra` and the **union of chart-scoped** Hydra ConfigMap `data.hydra` documents (see [`commands`](../../../develop/hydra-go/commands.md) — template patches). When the diff selection is **not** the full cluster app set, Hydra performs an extra **`RenderClusterSelectedApps` over all cluster apps** so Hydra config ConfigMaps owned by apps outside the selection (for example under Argo CD) still contribute rules. Identity and Hydra ConfigMap mutation guards match [`hydra gitops apply`](apply.md).

**Diff ignore rules (`global.hydra.diff.ignore`)** — Before comparing YAML, Hydra applies optional rules from merged Helm `global.hydra` and Hydra ConfigMap `data.hydra`: each rule has a CEL **`predicate`** and usually a list of **`yq:`** patches (mikefarah/yq v4) run on each compared document. Optional **`ignoreWhenMissingInCluster: true`** suppresses the unified diff for matching resources that exist only in templates (not in the cluster) and prints a one-line **`diff ignored (resource absent in cluster): …`** message instead; when the resource exists in the cluster, the normal diff applies. A built-in rule ignores `spec.replicas` for `Deployment`, `StatefulSet`, and `ReplicaSet` so replica drift matches [`hydra gitops apply`](apply.md) classification. See developer docs: [`diff-ignore-rules-implementation.md`](../../../develop/hydra-go/details/diff-ignore-rules-implementation.md).

For most production workflows, `diff` should be the mandatory review step before `apply`.

## When To Use It

Use `hydra gitops diff` when you need to answer one of these questions:

- What will Hydra change on the cluster?
- Is the live state drifting from the rendered state?
- Did a values or chart change affect only the resources I expect?

If you only need **offline** local rendering, use [`hydra local template`](../local/template.md). If you need **printed manifests** (no diff) with apiserver-preferred `apiVersion` values and `templatePatches` merged from Hydra ConfigMaps on **all** cluster apps, use [`hydra gitops template`](template.md).

## Diff Modes

| Mode     | Best for                                  | Notes                                                                                                         |
| -------- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `server` | Operational review before apply           | Default. Uses server-side apply semantics, so defaults and merge behavior are closer to the real apply result |
| `raw`    | Fast local inspection and troubleshooting | Direct YAML comparison. Faster, but can show noise from defaulted fields or apiserver-managed mutations       |

If the diff output is unexpectedly noisy, compare `server` and `raw` modes to understand whether the noise comes from Kubernetes defaulting or from the rendered manifests themselves.

In `server` mode, if server-side apply dry-run fails for any resource, the command exits with an error after logging per-resource warnings (including immutable conflicts—`hydra gitops apply` can handle those automatically, but diff does not). Use `--diff-mode raw` to compare templates to the cluster without that dry-run step, or fix the manifest; use [`--replace`](apply.md) on apply only when you need delete-before-apply for failures that are not API-reported immutable conflicts.

## Arguments

| Argument | Description                                                                                |
| -------- | ------------------------------------------------------------------------------------------ |
| `appId`  | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--dry-run` | `-d` | Simulate without cluster connection |
| `--no-cluster` | | Skip cluster connection (use with `--dry-run`) |
| `--diff-mode` | | `server` (default) — compares against server-side apply result, showing the effective diff including defaulted fields. `raw` — direct YAML comparison, faster but may show false positives from server-side defaults. |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--crd-mode` | | CRD handling: `error` or `ignore` |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--after-context` | `-A` | Unchanged lines to show **after** each change in unified diff hunks (default when unset: 3; same idea as grep) |
| `--before-context` | `-B` | Unchanged lines to show **before** each change in unified diff hunks (default when unset: 3) |
| `--hydra-context` | `-C` | Unchanged lines to show **before and after** each change (default when unset: 3; same idea as `grep -C`) |

## Examples

```bash
# Diff all child apps under infra
hydra gitops diff prod.infra.*

# Colored diff for readability
hydra gitops diff prod.infra.* --color

# Raw YAML comparison (faster, but may show server-side defaults as changes)
hydra gitops diff prod.infra.cert-manager --diff-mode raw

# Server-side diff for the most realistic pre-apply view
hydra gitops diff prod.infra.cert-manager --diff-mode server

# Diff only Deployments across all apps
hydra gitops diff prod.apps.* --include 'kind == "Deployment"'

# Diff everything except kube-system resources
hydra gitops diff prod.infra.* --exclude 'namespace == "kube-system"'

# Review a single app before applying it
hydra gitops diff prod.apps.my-service
hydra gitops apply prod.apps.my-service

# Tighter diff (less surrounding unchanged YAML), grep-style
hydra gitops diff prod.apps.my-service -C 0
```

## See Also

- [`hydra gitops apply`](apply.md) — apply the changes shown by diff
- [`hydra gitops dump`](dump.md) — inspect live cluster state directly
- [`hydra local template`](../local/template.md) — inspect rendered manifests without cluster connection
- [`hydra gitops template`](template.md) — print rendered manifests with cluster API normalization and cluster-wide `templatePatches`
