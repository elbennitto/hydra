# hydra gitops apply

Render and apply Hydra-managed resources to a Kubernetes cluster using server-side apply.

## Synopsis

```text
hydra gitops apply <appId> [appId...] [flags]
```

## Description

Renders the selected applications and applies the resulting manifests to the target cluster using [server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/). This is the main deployment command in Hydra.

Hydra does **not** print a unified diff for `cluster apply`. To review changes as a diff, use [`hydra gitops diff`](diff.md).

For the cluster side of planning and orphan detection, `hydra gitops apply` now reads live objects from the same resource model used by other cluster commands. Rendering and apply classification still have their own phases, but the live snapshot is consumed as normalized per-ID inventory entities rather than a separate command-local live-only slice.

After CLI flags are resolved (including any bundle implied by `--bootstrap`), Hydra resolves your app ID patterns against the repository, logs the **final sorted app ID list** for this run, then prints a fixed **optional apply behaviors** table (each optional behavior, its **effective** state for this run (`true` / `false`, or the effective `--sync` mode), and the CLI flags to turn that behavior on or off). That happens before Hydra renders templates or contacts the cluster. The table is printed on every apply (including `--no-cluster`), not only when `--bootstrap` is used. When **`--bootstrap` is not set**, Hydra logs one additional line after the table suggesting `--bootstrap` to enable the full bundle and `--no-*` to opt out; that line is omitted when **`--bootstrap` is set**.

Hydra logs orphaned resources (when applicable), prints the one-line **apply plan** summary (counts for new, update, replace, unchanged, delete, and how many replace operations need `--replace` when the failure is not an API-reported immutable field), and aborts if any such non-immutable replace is required and `--replace` is not set.

Unless **`--bootstrap`** or **`--skip-ref-checks`** is set, Hydra then validates **references against the simulated post-apply inventory**: it merges rendered objects with cluster objects that are **not** scheduled for orphan deletion (and adds synthetic namespace defaults where applicable), resolves virtual targets, then fails if any reference from the selected rendered sources would still lack a target or a referenced Secret/ConfigMap key. When **`--backup-restore`** is active and **`--skip-backup-restore`** is not set, **v1/Secret ids discovered as integrated backup restore targets** are treated as present for **existence** checks (the same discovery as the restore phase). **Referenced keys** inside those backup secrets are **not** loaded from backup files during this step—explicit key refs may still fail until manifests or live objects expose the keys. Use **`--bootstrap`** when initial setup steps (such as **`--sops-decode`**) must run so derived Secrets exist before dependents apply; use **`--skip-ref-checks`** only when you accept skipping this validation.

Phase log lines include `phase x/y`, a description, and a stable phase id in parentheses (for example `(apply-crds)`, `(apply-namespaces)`). The `x/y` numbering depends on which optional phases are enabled; refer to the id in docs and automation, not the numbers alone. Optional phases appear only when their flags are set; if a phase runs but has nothing to do for the current inputs, it is logged as **skipped**.

When **color output is enabled** and **stderr is a TTY**, Hydra shows **progress** on a **footer bar** at the bottom of the terminal (via [mpb](https://github.com/vbauerster/mpb)): first while **rendering Helm templates** for each selected app (`helm templates · apps`, one step per app), then while **preparing the apply** (`prepare · apply`, one step per pipeline stage between render and cluster listing—CRD checks, bootstrap guard, optional SOPS decode, clones, rule loading, references, patches, filters, manifest splits, optional backup checks, cluster client setup—with the **current step name** as detail), then while **listing the cluster** (`discovery`, one step per **API resource type** from discovery — same list the apiserver reports — with the current GVK shown as detail; with **`--parallel N`**, **`N = 0`** uses [GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS) (capped at **64**); when the effective **`N` is greater than `1`**, Hydra lists up to **`N`** resource types concurrently and the footer shows **one status line per worker**), then for each **apply** batch (including **`dry-run apply`** when `--dry-run` is set). During **apply planning**, the server-side apply dry-run that classifies existing resources (`ssa dry-run (planning)`) runs with **`--parallel N`** concurrent workers when **N** is greater than **1**; the footer shows **one status line per worker** under the bar. Completed footer phases are cleared so the bar does not stay on screen over later logs. Regular log lines still go to stderr above the bar.

Hydra validates the kubeconfig context before connecting. **Optional behaviors** (SOPS decoding, down-scaled apply, dependency-ordered scale-up, integrated backup restore, orphan scale-down before delete, ArgoCD AppProject sync policy, bootstrap-guard enforcement, bootstrap-tagged clone rules, and the non-ready webhook disable phase) are controlled by explicit flags. Unless you pass those flags, those steps do not run.

**`--bootstrap` and `--no-*`:** With **`--bootstrap`**, Hydra enables the full optional apply bundle for that run, except where you opt out using **`--no-sops-decode`**, **`--no-down-scaled`**, **`--no-scale-up`**, **`--no-orphan-scale-down`**, **`--no-bootstrap-guard`**, **`--no-bootstrap-clones`**, **`--no-backup-restore`**, or **`--no-disable-webhooks`**. Each **`--no-*`** flag is only valid together with **`--bootstrap`**. You must not combine **`--bootstrap`** with the positive optional flags (for example **`--down-scaled`**); use **`--bootstrap`** plus **`--no-*`** to tune the bundle. Pass **`--sync=...`** together with **`--bootstrap`** to set the ArgoCD sync policy (for example **`--sync=default`** overrides the bootstrap default **`keep-or-prevent`**; the optional behaviors table summarizes this in the **disable** column). For restore, **`--skip-backup-restore`** still works without **`--bootstrap`** and also skips restore when used with **`--bootstrap`**; **`--no-backup-restore`** is the bootstrap-style opt-out and participates in the same mutual exclusion group as **`--backup-restore`** / **`--skip-backup-restore`**.

**`global.hydra.templatePatches`** — Hydra collects patch rules from each app’s Helm `global.hydra` merged with the **union of chart-scoped** Hydra ConfigMap `data.hydra` documents (same union idea as `hydraAppMergedValuesMap` for effective Helm values). This differs from `global.hydra.diff.ignore`, where **`HydraDiffIgnoreRuleEntries`** still merges each app’s Helm map with **only that app’s** Hydra ConfigMap documents. When the apply selection is **not** the full cluster app set, Hydra runs an additional **`RenderClusterSelectedApps` over all cluster apps** for Hydra ConfigMap discovery so carriers from non-selected apps still merge. Hydra applies template patches once during rendering before scope validation, so patches can fix invalid raw chart output such as `metadata.namespace` on a cluster-scoped resource. After render, optional SOPS decode, and clone materialization, Hydra applies the compiled rules again to the working manifest set **before** CRD/namespace splitting and apply classification. Final-pass patches must not change resource identity or mutate Hydra configuration ConfigMaps; violations abort with an error. Use this for post-render mutations such as Argo CD `sync-options` on CRDs (see [`templatePatches in Values`](../../appendix/values/template-patches.md)).

**Always-on (for a normal cluster apply):** CRD phase, namespace phase, applying webhook configurations, a final phase that **deletes orphaned resources** when you do **not** use `--include` / `--exclude` (cluster objects no longer in templates for the selected apps—including webhooks, workloads, and other tracked types), and application of **main workload resources** (everything except webhooks) at **template scale**—replica counts and job suspend state match the rendered templates, without the scale-zero / scale-up choreography. When resource filters are set, Hydra applies only matching rendered objects, **lists only matching live cluster objects** (API lists that do not match the filter are skipped), logs how many rendered resources matched after `global.hydra.templatePatches`, and **skips orphan detection and orphan deletion** for that run so filtered-out objects are not treated as garbage. When **`--disable-webhooks`** is set (or implied by **`--bootstrap`**), webhook configurations join the downscaled/upscaled choreography: they are applied early with **`failurePolicy: Ignore`**, then enabled later in provider dependency order.

Phase log lines use `phase {current}/{total}` where **`total` depends on which optional steps are included** (for example backup restore, non-ready webhook disable, scale-up, and orphan scale-down are only phases when their flags are set, or when implied by `--bootstrap` where applicable). Steps that are not part of the run are omitted entirely—they are not listed as `(skipped)`.

**`--bootstrap`:** Shorthand that enables **all** optional flags below unless you opt out with a matching **`--no-*`** flag. Mutually exclusive with `--skip-bootstrap-guard` and **`--skip-ref-checks`**. If you pass `--skip-backup-restore`, integrated backup restore stays disabled even when using `--bootstrap`. If you pass `--no-backup-restore`, integrated backup restore stays disabled for that bootstrap run. `--backup-restore`, `--skip-backup-restore`, and `--no-backup-restore` are mutually exclusive.

**`global.hydra.clones`:** Tagless clone rules always materialize. Rules tagged `bootstrap` materialize only when **`--bootstrap-clones`** is set (or implied by `--bootstrap`).

Clone rules resolve each **target namespace** to a single owning application. Kubernetes **system** namespaces (`kube-system`, `kube-public`, `kube-node-lease`, and any namespace whose name starts with `kube-`) are **never** treated as owned by an app for this step, so several apps deploying into `kube-system` does not cause an **ambiguous app owners** error. If you need to disambiguate a **non-system** shared namespace (for example an operator namespace used by more than one app), declare it under `global.hydra.ownerNamespaces` in one app as described in the developer documentation for resource cloning.

**Bootstrap guard:** When **`--bootstrap-guard`** is set (or implied by `--bootstrap` without **`--no-bootstrap-guard`**), resources tagged `bootstrap-guard` in `global.hydra.refs` cause the command to fail unless you use `--bootstrap` or **`--skip-bootstrap-guard`**. You cannot use `--bootstrap-guard` and `--skip-bootstrap-guard` together. With **`--bootstrap`**, you can disable guard enforcement using **`--no-bootstrap-guard`** (different from **`--skip-bootstrap-guard`**, which remains mutually exclusive with **`--bootstrap`**). If `--skip-bootstrap-guard` is set but no guarded resource is in the selection, Hydra logs a **warning** because the flag had no effect.

**Integrated backup restore** runs only when **`--backup-restore`** is set (or implied by `--bootstrap` without `--skip-backup-restore`). Namespace metadata still matters for where the Secret is written and for validating backup ownership, but backup file paths are not used to decide which backups belong to the operation.

For day-to-day operations, the normal path is:

1. Validate cluster context.
2. Run [`hydra gitops diff`](diff.md).
3. Run `hydra gitops apply` with the **optional flags** you need (see [Flags](#flags)). To approximate the previous default choreography on existing clusters, pass at least `--down-scaled`, `--scale-up`, `--orphan-scale-down`, `--backup-restore`, and `--bootstrap-guard` when your selection may include guarded resources.

## When To Use It

Use `hydra gitops apply` when the desired state in the Hydra context should become the live cluster state.

Use another command when your goal is different:

- Use [`hydra local template`](../local/template.md) to inspect manifests locally, or [`hydra gitops template`](template.md) when you want the same cluster-side normalization and cluster-wide `templatePatches` behavior as apply/diff (read-only).
- Use [`hydra gitops diff`](diff.md) to review changes first.
- Use [`hydra gitops scale`](scale.md) for temporary stop/start operations.
- Use [`hydra gitops uninstall`](uninstall.md) for removal.

## CRD scope and safety

Hydra needs CustomResourceDefinitions to know whether a custom API is namespaced or cluster-scoped and to normalize manifests. For `hydra gitops apply`, that scope information may be derived from CRDs found in **any** app rendered for the same cluster in your Hydra context, not only from the app IDs you pass on the command line. For example, a custom resource in one chart may be defined by a CRD that is packaged with another app in the same cluster.

Applying manifests is still limited to the apps you select. A required CRD must be **available** before the rest of the apply can succeed: it must **either** already exist on the target cluster **or** be part of the manifests for the apps you are applying (so it can be installed in the CRD phase of the same run). If you apply an app that emits instances of a custom API whose CRD is only present in a different app that you did not include, and that CRD is not already established on the cluster, the command **fails** instead of applying an incomplete set. Use [`hydra gitops diff`](diff.md) to review the change set; widen the `appId` list or install the CRD out of band if the operator chart must be applied first.

With the default `--crd-mode error`, unresolved API definitions surface as errors. With `--crd-mode ignore`, Hydra may skip resources it cannot classify; it does not change the rule that you cannot safely apply instances whose CRDs will not be installed or are not already present.

## Operational Notes

- `apply` is **mutating** and talks to the live cluster unless combined with `--dry-run --no-cluster`.
- During the **scale-up** phase (only when **`--scale-up`** is set, which requires **`--down-scaled`**), **batch Jobs** participate in the same dependency-ordered startup as Deployments, StatefulSets, and DaemonSets: manifests are applied with `spec.suspend: true` in the down-scaled apply phase, then each Job is unsuspended in topological order after its dependencies are ready, and Hydra waits for the Job to finish successfully (or aborts if the Job fails). Jobs owned by a CronJob are excluded (the CronJob controller owns their lifecycle). This requires Kubernetes 1.22+ (suspended Jobs).
- During the webhook-enable phase (only when **`--disable-webhooks`** is set, including via **`--bootstrap`**), webhook configurations are treated like bootstrap-managed workloads: Hydra applies them early with `failurePolicy: Ignore`, then re-applies their normal template state later in the dependency order of their backing providers. This avoids races with Helm-style bootstrap Jobs that expect the webhook objects to exist before the provider is fully ready.
- Server-side apply means the apiserver calculates the effective result, including defaults and merge behavior.
- **`--bootstrap`** is intended for initial setup or recovery paths; it turns on the full optional apply package unless you opt out with **`--no-*`** flags.
- Duplicate object ids in the rendered manifests are still handled as elsewhere: Hydra logs a **warn** and keeps the last object only. **Exception:** with **`--sops-decode`**, an active integrated backup restore phase (**`--backup-restore`** and **not** `--skip-backup-restore`), if the same `v1/Secret` id would be written both by **backup restore** (from a backup `SopsSecret`) and by the rendered apply set (for example a plain `Secret` from templates or a Secret derived from a non-backup `SopsSecret`), the command **fails** after render completes (after the `found definitions of …` log) and **before** the cluster resource listing (`listing all resources of cluster …` or, with `--include` / `--exclude`, `listing cluster … resources matching resource filters`). The error lists the conflicting id and both sources so you can remove or consolidate one side. This check does not run when backup restore is skipped or when there are no backup restore candidates.
- When **`--backup-restore`** and **`--sops-decode`** are both active, automatic backup restore discovers backup inputs only from the selected app IDs. Ownership-invalid backups are reported as `skipped`. Backup `SopsSecret` resources that are out of scope for the integrated restore are also removed from the later main apply set (same bootstrap-style filtering as before).
- Use `--skip-backup-restore` when the integrated backup restore phase should be skipped entirely for this apply run (cannot be combined with `--backup-restore` on the same command line).
- When **`--sync`** leads to at least one ArgoCD `AppProject` sync change, a short completion hint points to `hydra argocd sync` / `hydra gitops sync`. If no `AppProject` needed changes, that message is omitted.
- SSA dry-run runs first (use **`--parallel N`** for **N** concurrent dry-run patches during planning and, earlier in the pipeline, for **N** concurrent discovery list workers; **`N = 0`** means [GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS), capped at **64**; default **`0`**). The Kubernetes client still applies a default REST **QPS** limit (~**5** sustained, **10** burst) unless you raise or disable it with **`--qps`** / **`--api-burst`** (same tuning applies to listing, SSA dry-run planning, and apply HTTP traffic in this command). Change detection uses the same **`global.hydra.diff.ignore`** normalization as [`hydra gitops diff`](diff.md): CEL **`predicate`** plus **`yq:`** patches on each side before YAML compare. A built-in rule removes `spec.replicas` for Deployments, StatefulSets, and ReplicaSets so pure replica drift does not force a re-apply. If a workload was **scaled to zero** in the cluster (`spec.replicas: 0`) while the template expects a **positive** replica count, that case **is** still detected and the next apply restores template scale.
- With **`-v` / `--verbose`** (global), Hydra emits **DEBUG** lines for each **existing** resource after the SSA dry-run classification step: stable `logId` `hydra.cli.action.apply-dry-run-diff`, resource `id`, and `result` `unchanged` or `changed` (optional `reason`: `yaml_diff`, `missing_dry_run`, `restore_replicas_after_scale_zero`). Use this to grep a specific `id` and align with [`hydra gitops diff`](diff.md); it is not a full unified diff.
- Successful apply lines for each object include the Hydra resource **id** (full `apiVersion` / kind / namespace / name path) together with the short `resource` form, so objects that share a name in different namespaces are unambiguous in the log.
- Namespaced **`ConfigMap/kube-root-ca.crt`** objects maintained by the Kubernetes apiserver are **not applied** (they are removed from the rendered set before apply). If templates still emit them, Hydra logs how many were excluded. These objects are also **not** selected as ArgoCD-tracked orphans for deletion solely because they disappeared from templates.
- When the Kubernetes API reports an **immutable field** conflict, Hydra deletes and recreates **only** those objects before apply (no `--replace` needed). Use **`--replace`** when dry-run fails for **other** reasons and you still want delete-before-apply for those resources—accept brief removal where applicable. **`--replace` is not allowed together with `--include` / `--exclude`.**
- **`--include` / `--exclude`** follow the same CEL resource filter model as [`hydra gitops diff`](diff.md). After template patches, Hydra logs **`apply resource filter: N rendered resource(s) match … (from M after template patches)`**, then lists the cluster using only API resource types and objects that match the filter, and logs **`listed K cluster resources matching resource filters …`**. The server-side apply dry-run for classification uses only the **intersection** of filtered template objects with filtered live objects (so counts align with `N` / `K`, not the full cluster inventory). They cannot be combined with **`--orphan-scale-down`** (which opts into scaling down orphans before delete) because that path deletes cluster resources in ways that conflict with a partial apply selection; **`--bootstrap`** implies **`--orphan-scale-down`** unless you pass **`--no-orphan-scale-down`** or set **`--orphan-scale-down=false`** on the command line while keeping other bootstrap-implied flags.
- If you run `hydra gitops backup restore` separately and the selected backups target namespaces that do not exist yet, use `--create-namespaces`.
- If ArgoCD is actively reconciling and you are doing temporary maintenance work, manage sync mode first with [`hydra argocd`](../argocd/README.md).

## `--sync` (ArgoCD AppProject sync)

Unless overridden, **`--sync` defaults to `default`**. With **`--bootstrap`** and **without** **`--sync`**, the default is **`keep-or-prevent`**.

| Value | Meaning |
| ----- | ------- |
| `default` | For every `AppProject` in this apply’s workload set, keep the sync configuration from the rendered template (no extra mutation). |
| `manual` | Same as **`hydra gitops sync manual`**: manual sync only (yellow in ArgoCD UI). |
| `auto` | Same as **`hydra gitops sync auto`**: automatic sync enabled (green in ArgoCD UI). |
| `prevent` | Same as **`hydra gitops sync prevent`**: all sync blocked (red in ArgoCD UI). |
| `keep-or-manual` | Do **not** change sync for `AppProject` resources that already exist in the cluster; **new** `AppProject` resources get the **`manual`** mapping above. |
| `keep-or-auto` | Same as `keep-or-manual`, but new projects get the `auto` mapping. |
| `keep-or-prevent` | Same as `keep-or-manual`, but new projects get the `prevent` mapping. |
| `keep-or-default` | Same as `keep-or-manual`, but new projects keep the template’s sync definition. |

Deprecated aliases (accepted for compatibility): `deny` → `manual`, `keep-or-deny` → `keep-or-manual`.

With **`--bootstrap`** and **without** **`--sync`**, Hydra uses **`keep-or-prevent`**. To use template sync only (same as mode **`default`**), pass **`--sync=default`** on the same command line. The **optional apply behaviors** table shows the effective mode and reminds you that **`--sync=default`** overrides the bootstrap **`keep-or-prevent`** default.

## Arguments

| Argument | Description                                                                                |
| -------- | ------------------------------------------------------------------------------------------ |
| `appId`  | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag                       | Short | Description                                                                                            |
| -------------------------- | ----- | ----------------------------------------------------------------------------------------------------   |
| `--hydra-context`          |       | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var)     |
| `--color`                  | `-c`  | Force colored output                                                                                   |
| `--dry-run`                | `-d`  | Simulate the apply without making changes (sends manifests through server-side apply dry-run)          |
| `--no-cluster`             |       | Skip cluster connection entirely (use with `--dry-run` to validate rendering only)                     |
| `--helm-network-mode`      |       | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error`          |
| `--no-cache`               |       | Disable persistent Helm template cache and in-process Helm-related caches for this run                 |
| `--crd-mode`               |       | CRD handling: `error` (fail if CRDs are missing) or `ignore` (skip CRD-dependent resources)            |
| `--bootstrap`              |       | Optional apply bundle; use `--no-*` to opt out; not with `--skip-bootstrap-guard`                      |
| `--no-sops-decode`         |       | With `--bootstrap`, skip implied SOPS decode (requires `--bootstrap`)                                  |
| `--no-down-scaled`         |       | With `--bootstrap`, skip implied down-scaled apply (requires `--bootstrap`)                            |
| `--no-scale-up`            |       | With `--bootstrap`, skip implied scale-up phase (requires `--bootstrap`)                               |
| `--no-orphan-scale-down`   |       | With `--bootstrap`, skip implied orphan scale-down (requires `--bootstrap`)                            |
| `--no-bootstrap-guard`     |       | With `--bootstrap`, skip implied bootstrap-guard enforcement (requires `--bootstrap`)                  |
| `--no-bootstrap-clones`    |       | With `--bootstrap`, skip implied bootstrap-tagged clones (requires `--bootstrap`)                      |
| `--no-backup-restore`      |       | With `--bootstrap`, skip implied restore; exclusive with `--backup-restore` / `--skip-backup-restore`  |
| `--no-disable-webhooks`    |       | With `--bootstrap`, skip implied webhook-disable phase (requires `--bootstrap`)                        |
| `--sops-decode`            |       | Decrypt `SopsSecret` CRs to plain `Secret` objects (do not combine with `--bootstrap`)                 |
| `--down-scaled`            |       | Apply workloads at scale zero; pair with `--scale-up` (do not combine with `--bootstrap`)              |
| `--scale-up`               |       | After down-scaled apply, scale up (**requires** `--down-scaled`; not with `--bootstrap`)               |
| `--orphan-scale-down`      |       | Scale down orphans before delete (do not combine with `--bootstrap`)                                   |
| `--sync`                   |       | `AppProject` sync — see [`--sync`](#--sync-argocd-appproject-sync)                                     |
| `--bootstrap-guard`        |       | Enforce bootstrap-guard refs (not with `--bootstrap` or with `--skip-bootstrap-guard`)                 |
| `--bootstrap-clones`       |       | Materialize `global.hydra.clones` rules tagged `bootstrap` (do not combine with `--bootstrap`)         |
| `--backup-restore`         |       | Integrated backup restore (exclusive with `--skip-backup-restore` / `--no-backup-restore`)             |
| `--skip-bootstrap-guard`   |       | Skip bootstrap-guard (mutually exclusive with `--bootstrap` and `--bootstrap-guard`)                   |
| `--skip-ref-checks`        |       | Skip post-plan reference validation (not with `--bootstrap`)                                           |
| `--scale-timeout`          |       | Timeout for scale operations (e.g. `10m`) when workloads must become ready                             |
| `--crd-timeout`            |       | Timeout waiting for CRDs to become established (e.g. `60s`)                                            |
| `--force-backup-restore`   |       | Force restore of backed-up secrets even when they differ from cluster state                            |
| `--skip-backup-restore`    |       | Skip backup restore phase (exclusive with `--backup-restore` / `--no-backup-restore`)                  |
| `--disable-webhooks`       |       | Apply webhook configs early with `failurePolicy: Ignore`, then enable them later in provider order     |
| `--exclude-app`            |       | Glob pattern to exclude applications (repeatable)                                                      |
| `--replace`                |       | Delete-before-apply for non-immutable SSA dry-run failures too (immutable: automatic)                  |
| `--include`                | `-i`  | [CEL resource filter](../README.md#cel-resource-filters) (repeatable); see Operational Notes           |
| `--exclude`                | `-e`  | [CEL resource filter](../README.md#cel-resource-filters) (repeatable); same restrictions as `-i`       |
| `--parallel`               |       | Discovery+SSA concurrency (**`0`**=[GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS); max **64**)    |
| `--qps`                    |       | REST client QPS (**0** default; **negative** = unlimited client-side)                                  |
| `--api-burst`              |       | Burst when `--qps` > **0**; **requires** `--qps`; not with negative **`--qps`**                        |

## Pre-Flight Checklist

Before running `apply` against a live cluster:

1. Run [`hydra gitops validate-current-context`](validate-current-context.md).
2. Review the change set with [`hydra gitops diff`](diff.md).
3. Decide whether this is a normal update or a bootstrap/recovery scenario.
4. If you made manual temporary changes, confirm ArgoCD sync settings will not immediately undo them.

## Dry-Run Modes

These flags are easy to confuse:

| Command shape                                    | Meaning                                                          |
| ------------------------------------------------ | ---------------------------------------------------------------- |
| `hydra gitops apply ... --dry-run`               | Render and send the apply request to the cluster in dry-run mode |
| `hydra gitops apply ... --dry-run --no-cluster`  | Render only, without talking to the cluster                      |
| `hydra gitops apply ...`                         | Render and apply for real                                        |

## Examples

```bash
# Apply a single child app
hydra gitops apply prod.infra.cert-manager

# Apply all child apps under a root app
hydra gitops apply prod.infra.*

# Dry-run to preview changes (server-side apply simulation)
hydra gitops apply prod.infra.* --dry-run

# Render only, without connecting to the cluster
hydra gitops apply prod.infra.* --dry-run --no-cluster

# Bootstrap a new cluster (implies SOPS decode, down-scaled apply, scale-up, orphans, sync keep-or-prevent by default, guard, clones, backup restore)
hydra gitops apply prod.** --bootstrap

# Bootstrap a new cluster, but preview the server-side result first
hydra gitops apply prod.** --bootstrap --dry-run

# Bootstrap without integrated backup restore
hydra gitops apply prod.** --bootstrap --skip-backup-restore

# Bootstrap with down-scaled apply but without dependency-ordered scale-up
hydra gitops apply prod.** --bootstrap --no-scale-up

# Typical cluster update with previous default choreography (no SOPS / default sync)
hydra gitops apply prod.apps.* --down-scaled --scale-up --orphan-scale-down --backup-restore --bootstrap-guard

# Skip integrated backup restore while bootstrapping
hydra gitops apply prod.** --bootstrap --skip-backup-restore

# Apply only Deployments (orphan cleanup skipped for this run)
hydra gitops apply prod.apps.* --include 'kind == "Deployment"'

# Bootstrap-style flags but narrow to one resource kind (disable implied orphan scale-down)
hydra gitops apply prod.** --bootstrap --no-orphan-scale-down --include 'kind == "Deployment"'
# Equivalent legacy form:
# hydra gitops apply prod.** --bootstrap --orphan-scale-down=false --include 'kind == "Deployment"'

# Apply everything except test apps
hydra gitops apply prod.apps.* --exclude-app "test-*"

# Apply after diffing a single app
hydra gitops diff prod.infra.cert-manager
hydra gitops apply prod.infra.cert-manager

# Immutable field conflicts (e.g. Secret type) are handled automatically after SSA dry-run
hydra gitops apply prod.infra.my-app

# Force delete-before-apply for other SSA dry-run failures as well
hydra gitops apply prod.infra.my-app --replace
```

## See Also

- [`hydra gitops diff`](diff.md) — preview what would change before applying
- [`hydra gitops validate-current-context`](validate-current-context.md) — verify cluster context
- [`hydra gitops backup`](backup.md) — backup secrets before destructive operations
- [`hydra local template`](../local/template.md) — inspect rendered manifests locally
- [`hydra gitops template`](template.md) — inspect rendered manifests with API normalization and cluster-wide `templatePatches`
