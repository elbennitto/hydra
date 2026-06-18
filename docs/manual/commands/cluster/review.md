# hydra gitops review

Review rendered Hydra applications against live cluster targets.

## Synopsis

```bash
hydra gitops review app <appId> [appId...] [flags]
hydra gitops review cluster <cluster> [flags]
```

## Description

The `cluster review` command family is the cluster-connected counterpart to [`hydra local review`](../local/review.md).

For the cluster-side command path, Hydra now reads template and live entities from the shared resource model: rendered sources and live targets still come from different build phases, but review consumes one normalized per-ID inventory view instead of stitching together separate command-local template and live collections.

With **color output** and a **TTY stderr**, the initial **full-cluster list** used for review shows the same **`discovery`** footer progress bar as [`hydra gitops apply`](apply.md) (one step per listable API resource type, current GVK as detail). After discovery, **`cluster review · post-discovery`** is a single footer bar that covers both the outer preparation steps and the reference-validation substeps (template sources vs live cluster, key enrichment, target index, and scan). Ref ownership uses one **`cluster review · ref ownership`** bar for the coarse 8-step flow. When the effective **`--parallel`** value is greater than `1`, the two CPU-heavy passes each open their own footer bar under it: **`cluster review · template vs ref ownership`** (per-template-id work) and **`cluster review · assign live resources`** (per live inventory object), each with one worker status line per parallel worker under that sub-bar.

- **`hydra gitops review app`** — Pass one or more [App IDs](../README.md#app-ids) (with [wildcards](../README.md#wildcards)). Hydra resolves the apps, then builds a source candidate set from the rendered templates **and** clone materializations of **those** apps only, applies API version normalization so template IDs match cluster IDs, then **filters** that candidate set to only include resources whose IDs also exist on the live cluster. Reference findings and ref-ownership findings are limited to that app selection. In particular, **`ref ownership: cluster-only resource has no Hydra app assignment`** is **not** emitted here; use **`hydra gitops review cluster`** when you need that audit.

- **`hydra gitops review cluster`** — Pass a **cluster name** (a single path segment: **no `.`** — it is **not** an app id). Hydra loads **every** application defined for that cluster in the repository, optionally narrowed by **`--exclude-app`**, and runs the same review pipeline over **all** those apps’ rendered sources (filtered to objects that exist on the live cluster). The live **target** inventory is still the full cluster snapshot. This mode **does** emit **`ref ownership: cluster-only resource has no Hydra app assignment`** for eligible live objects (same namespace scope as [`hydra gitops uninstall`](uninstall.md) leftover scanning).

In both modes, references are validated primarily against **live cluster entities**. For **namespace-local** references to `ServiceAccount/default` or `ConfigMap/kube-root-ca.crt` in the **same** namespace as the namespaced source, Hydra may also accept targets synthesized from the **full enabled-app template render** (so offline manifests model defaults the API creates per namespace). Cluster-scoped sources and cross-namespace references still require a live object. Resources that are rendered but not yet deployed to the cluster are **not** reviewed, but references from reviewed sources to targets that do not exist on the cluster (for example a deployed `Deployment` referencing a `Secret` that has not been created yet) are still reported.

Use this command when you need to know whether referenced objects and keys exist in the API for resources that are already deployed, including targets that other teams or controllers manage outside your selected render set.

The review validates:

- **Kubernetes bootstrap defaults** — Only **`hydra gitops review cluster`** runs a post-pass that compares the **live** cluster inventory to the same **upstream** bootstrap catalog as local review (initial namespaces, core ServiceAccounts / ConfigMaps / `kubernetes` Service, RBAC bootstrap `ClusterRole` / `ClusterRoleBinding` names). Each expected object that is **absent** is reported with the message **`missing cluster default resource`** (empty `sources`). **`hydra gitops review app`** omits this full-catalog audit (same scope split as unassigned ref-ownership findings). The catalog is filtered by the API server’s **minor** version from discovery so older clusters are not expected to include RBAC objects introduced only in newer releases. If the server version cannot be read, this audit is skipped. When the audit runs, references to a missing bootstrap object **do not** also emit `missing target resource`; the bootstrap finding covers that ID. The catalog is **not** distribution-specific (no k3s/k3d extensions). Namespace-local default synthesis from templates is **independent** of this audit and still applies when discovery cannot read a server version.
- **Per-namespace defaults from templates** — The same `default` ServiceAccount and `kube-root-ca.crt` ConfigMap bundle as in local review is derived from namespaces seen in the **enabled-app template render**. It supplies the standard `ca.crt` key for key-aware checks. It does **not** satisfy cross-namespace `RoleBinding` / `ClusterRoleBinding` subjects or other references where the target namespace differs from the source’s namespace.
- missing target resources such as `Secret` or `ConfigMap` reported as `missing target resource` when neither a matching live object exists nor a **recursive, fixpoint-stabilized** chain of refs whose attributes include **`"origin:generated": job`** or **`"origin:generated": controller`** (per app-declared parsers) accounts for that target, including multi-hop materialization
- missing explicitly referenced keys inside live `Secret` and `ConfigMap` targets (or their generated key sets when modeled)
- **Ref ownership** — after reference checks, every **template resource id** is evaluated like [`hydra local review`](../local/review.md): **`uninstall`** / **`uninstall-safe`** / **`uninstall-force`** / **`backup`** predicates **without** ref groups tagged **`runtime`**. Before that step, **`hydra gitops review`** normalizes **`apiVersion`** on the merged enabled-app render and on each app’s standalone render to the cluster’s preferred versions (same scope as source filtering), so template ids align with live objects even when charts still emit deprecated API versions (for example Strimzi **`Kafka`** / **`KafkaTopic`** at **`kafka.strimzi.io/v1`** while the manifest shows **`v1beta2`**). For **live** cluster objects, Hydra uses that same non-runtime predicate set when the object’s id **appears in a standalone template render**, so broad **`runtime`** rules never claim template-mapped resources. When the id appears in **no** template (cluster-only), the **full** predicate set **including** **`runtime`** applies (aligned with **`hydra gitops uninstall`** for non-template resources). **Live `v1/Namespace` objects** whose **`metadata.name`** is listed under **`global.hydra.ownerNamespaces`** for an app are assigned to that app (after ref predicates, before **`metadata.ownerReferences`** expansion) so they are not treated as unassigned solely because the chart did not render a **`Namespace`** manifest. If ref predicates already assigned a **different** app to that Namespace object, the review emits **`ref ownership: v1/Namespace ref assignment conflicts with global.hydra.ownerNamespaces`**. Hydra then applies the same **`metadata.ownerReferences`** propagation as **`hydra gitops uninstall`**: once a direct owner in the live inventory is assigned to an app, children inherit that app (transitive chains such as Pod → ReplicaSet → Deployment). If owner references resolve to **more than one** app, the review emits **`ref ownership ambiguous: owner references resolve to multiple apps`**. If more than one app matches ref predicates alone for a cluster-only id, the review emits **`ref ownership ambiguous for cluster-only resource`**. If a cluster-only object still has **no** app after predicates, **`ownerNamespaces`** matching (for **`v1/Namespace`** only), and owner propagation, and it lives in a namespace in the **uninstall leftover namespace set** (exclusive namespaces plus every namespace from enabled apps’ renders **plus every namespace declared in `global.hydra.ownerNamespaces`**), the review may emit **`ref ownership: cluster-only resource has no Hydra app assignment`** (the same class of object that would abort **`hydra gitops uninstall`** without `--force-all`) — **only** when you run **`hydra gitops review cluster`**, not **`hydra gitops review app`**, **unless** the object matches an active **`global.hydra.presets`** untracked rule (builtin **`coredns`** / **`kubernetes`** / **`flannel`** plus merged overrides): preset-only matches **suppress** that unassigned finding. When a cluster-only object **does** have a Hydra app assignment **and** matches an active untracked preset, the review additionally emits **`ref ownership: cluster-only resource matches untracked preset(s) and a Hydra app assignment`**. For that finding, objects whose **`metadata.ownerReferences`** include a UID that resolves to **another object in the live inventory** are skipped so only the **ownership root** of each chain is reported (not child objects such as Pods or ReplicaSets when the parent workload object is also unassigned). Kubernetes-managed defaults are excluded from that unassigned finding: the same **upstream bootstrap catalog** as the **`missing cluster default resource`** audit, plus **`ServiceAccount/default`** and **`ConfigMap/kube-root-ca.crt`** in **any** namespace (the objects the API injects per namespace, matching the synthetic bundle used for reference checks).
- repeated findings grouped by identical target and message

Optional Kubernetes references (`optional: true` on the same fields as in local review) are tagged `optional:ref` in the reference model and are **not** validated here yet (the UI graph shows that tag on edges).

Controller-managed objects often include **`metadata.ownerReferences`** on the **live** cluster; Hydra’s built-in parsers emit corresponding graph edges (labels `controller` / `owner`, attribute `origin:owner`). Templates usually omit owner references, so this is most visible when sources or targets come from the API.

Rendered sources may omit `metadata.namespace` on namespaced objects. Hydra normalizes those sources to the application's target namespace before running the same reference checks as local review, so source-side identities and lookups stay consistent while targets still come from the live API (with their real namespaces).

Applications with `enabled: false` are not rendered by Hydra, so they contribute **no reference sources** in this command. **Targets** still come only from the **live API**: the inventory is the full cluster snapshot Hydra loads, independent of whether an app is currently enabled in Git (objects from an earlier rollout may still be present on the cluster).

[`hydra local review`](../local/review.md) and `hydra gitops review` share the same reference-review implementation; cluster mode builds **sources** from rendered templates and clone materializations filtered to only resources present on the cluster, and supplies **targets** from the live cluster inventory (without clone materializations) plus the auxiliary per-namespace default bundle from the enabled-app template render for qualifying same-namespace references. In the current cluster command implementation, those template and live sides are read back out of the same resource model. For **StatefulSet** `volumeClaimTemplates`, cluster mode also feeds the **live API snapshot** into the CEL inventory used while parsing **sources**, so edges to **real** PVCs can be confirmed when `metadata.ownerReferences` shows the StatefulSet as controller (in addition to expected PVC names from the template). This overlay applies **only** to `hydra gitops review`, not to `hydra gitops refs` tree aggregation.

Hydra may still **collect and group** all findings in memory before anything is printed; stdout is filled **after** that, one finding at a time in final order. **By default** the text output **groups findings by message type** (for example unassigned cluster-only ref ownership uses one heading that includes the scope note, e.g. `ref ownership: cluster-only resource has no Hydra app assignment (would remain unassigned for hydra gitops uninstall in this namespace scope)`, with one **Target** line per resource and no redundant **Detail** line for that class), omits a **Sources** section when it would be empty, and may add a per-target **Message** or **Detail** line when the full text adds detail beyond the shared type. Use **`--yaml`** for the previous behavior: one YAML sequence element per finding, without marshaling the full list as a single YAML value at the end. You do not get partial findings while grouping is still running.

When findings exist, `hydra gitops review` exits with a non-zero status so it can be used in pre-apply checks or CI automation.

## hydra gitops review vs hydra local review

Use the cluster command when live API state must satisfy the references:

- [`hydra gitops review`](../cluster/review.md) builds **sources** from rendered templates and clone materializations of the selected apps, filtered to only resources present on the live cluster (with API version normalization), and resolves **targets** against live cluster entities only (without clone materializations). Only resources that actually exist on the cluster are reviewed, but missing references from those resources are still reported.
- [`hydra local review`](../local/review.md) renders the selected apps for **sources** and resolves **targets** against **all** enabled apps' templates in the Hydra context (offline).

Both modes allow cross-app targets as long as the target exists in the respective target set. Cluster mode additionally sees objects that never appear in Hydra templates but only reviews resources already deployed to the cluster.

## Subcommands

| Subcommand | Arguments | Purpose |
| ---------- | --------- | ------- |
| `app` | `<appId...>` | Review only the resolved app set (after `--exclude-app`). Omits unassigned cluster-only ref-ownership findings and the full-catalog **`missing cluster default resource`** bootstrap audit. |
| `cluster` | `<cluster>` | Review **all** apps defined for the named cluster in the repo (after `--exclude-app`). **Cluster** must not contain `.`. Includes unassigned cluster-only ref-ownership findings and the bootstrap default audit. |

Before running it, validate that your kubeconfig points at the intended cluster with [`hydra gitops validate-current-context`](validate-current-context.md).

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--exclude-app` | | Glob pattern to exclude applications from the **source** app set (repeatable); does **not** narrow the live **target** inventory. Allowed for both `app` and `cluster` subcommands. |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--yaml` | | Emit each finding as YAML (default: human-readable text) |
| `--color` | `-c` | Force colored output (text or YAML) |
| `--no-color` | | Disable colored output even in a terminal |
| `--color-mode` | | Color mode: `auto` (default, TTY-detected), `always`, or `never` |
| `--bootstrap` | | Include `global.hydra.clones` rules tagged `bootstrap` when building the source candidate set from clone materializations |
| `--parallel` | | Same as other cluster commands: worker count for the ref-ownership **template vs ref-parser** scan and the **live resource assignment** pass (and for **discovery** listing). With terminal progress and an effective value greater than `1`, each of those phases gets its own **sub** footer progress bar (under **`cluster review · ref ownership`**) with one worker status line per worker. **`0`** (default) means [GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS), capped at **64**. |
| `--qps` | | Kubernetes REST client QPS override inherited from `hydra gitops` (`0` = client-go default, negative disables client-side throttling) |
| `--api-burst` | | Kubernetes REST client burst override inherited from `hydra gitops` (requires positive `--qps`) |

`--color`, `--no-color`, and `--color-mode` are mutually exclusive. With no color flag, Hydra uses the same automatic behavior as other commands (color only when standard output is a TTY).

## What Gets Checked

- **Sources:** rendered template entities **and** clone materializations from the resolved `appId` set after `--exclude-app`, with API version normalization applied, then filtered to only include resources whose IDs also exist on the live cluster (only these may appear in finding `sources`). Resources that are rendered but not yet deployed are not reviewed.
- **Targets:** live cluster entities only, without clone materializations; `--exclude-app` does **not** filter this list. Both the source-side template view and the live target view are read from the shared resource model.
- Namespaced rendered resources without `metadata.namespace` are aligned to the app's namespace on the source side before references are evaluated, so cross-references resolve against the namespace where the app deploys; live targets keep their cluster-reported namespaces.
- A referenced `Secret` or `ConfigMap` is treated as present if it exists in the live inventory **or** app ref-parsers supply a **`"origin:generated": job`** / **`"origin:generated": controller`** explanation for it, **including multi-hop** materialization; otherwise Hydra reports `missing target resource`.
- Explicit key references such as `secretKeyRef`, `configMapKeyRef`, and keyed projected-volume items are validated against the resolved target (live object or generated key set).
- `envFrom` checks only that the referenced `Secret` or `ConfigMap` exists, because it does not point to a single named key.

## Output and logs

- **Stdout** carries finding output only when there are findings: **default** human-readable text per finding, or with **`--yaml`** the YAML sequence of findings. With zero findings, stdout has no finding payload.
- When there are findings, Hydra also logs a one-line **ERROR** summary with the total count (so you see how many issues were reported without relying on stderr from the CLI layer). With zero findings, an **INFO** line confirms that no reference issues were found.
- **Stderr / log level** carries operational messages. For timing-oriented **debug** messages after Helm templating (reference parsing, ref graph build, key enrichment, target-key normalization, grouping or sorting), enable debug logging with the global `--verbose` (`-v`) flag described under [Global Flags](../README.md#global-flags).

## Examples

```bash
# Confirm kubeconfig points to the intended cluster
hydra gitops validate-current-context prod

# Review one app against live cluster targets
hydra gitops review app prod.infra.cert-manager

# Review all apps on one cluster except one child app
hydra gitops review app prod.** --exclude-app prod.infra.cert-manager

# Full-cluster app set for cluster "prod" (repo layout), optional excludes
hydra gitops review cluster prod --exclude-app prod.infra.cert-manager

# Review a subset with offline Helm dependency handling
hydra gitops review app prod.apps.* --helm-network-mode offline

# Machine-readable YAML findings (legacy format)
hydra gitops review app prod.apps.* --yaml
```

## See Also

- [`hydra local review`](../local/review.md) - validate references offline against all enabled apps' templates on the cluster
- [`hydra gitops diff`](diff.md) - compare rendered state against live cluster state
- [`hydra local template`](../local/template.md) - inspect rendered manifests without cluster connectivity
- [`hydra gitops validate-current-context`](validate-current-context.md) - confirm you are connected to the intended cluster
