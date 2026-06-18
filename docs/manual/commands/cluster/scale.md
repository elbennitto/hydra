# hydra gitops scale

Scale workloads in a Kubernetes cluster up or down.

## Synopsis

```text
hydra gitops scale <up|down|status> <appId> [appId...] [flags]
```

## Description

Manages the replica count of workloads (Deployments, StatefulSets, etc.) in the cluster.

The command's live cluster reader now comes from the shared resource model used by other `hydra gitops` commands. Template-defined scale targets and live inventory are still merged for scaling decisions, but the live side is read from normalized per-ID inventory entities rather than a separate command-local cluster list.

**Custom operator CRs (`global.hydra.scale`):** Definitions can live in Helm chart defaults or in-cluster Hydra ConfigMaps, not only in the cluster `values.yaml` on disk. For the selected apps, Hydra merges **`global.hydra.scale`** using the same model as ref-parsers (per-app Helm `global.hydra` plus rendered **`data.hydra`** documents, plus cluster-level values). Chart-defined entries (for example next to the operator that ships the CRD) therefore participate in **`scale up`**, **`scale down`**, and **`scale status`** once those apps are included in the command’s app selection.

**Scale up and readiness:** `hydra gitops scale up` runs workloads in **topological** order (required dependencies first, then workloads reachable only via **`optional:startup`** refs). For each workload and for **transitive** dependencies along the ref graph, if a **`global.hydra.ready`** rule matches (including built-in defaults), Hydra **waits** until **all** CEL expressions in that rule **pass** before starting dependents that are blocked on that node. Each expression may return **`null`** (omit), **`""`** or **`[]`** (pass), a **non-empty string** (one failure reason), or a **non-empty list of strings** (multiple reasons). **`bool` is not a valid return type** for `global.hydra.ready` expressions. Entities **without** a matching ready rule are **not** subject to this extra gate; they still use the existing replica / `statusReadyPath` / Job completion waits where applicable. See [Readiness configuration](#readiness-configuration-globalhydraready).

- **`scale down`** — sets replicas to zero, effectively stopping all pods
- **`scale up`** — restores workloads to their target replica count (as defined in the rendered manifests)
- **`scale status`** — read-only: default **colored text** report of each scale target’s **sync** state versus template and scaled-down semantics, plus **ready** state when a rule matches, and dependencies with the same dimensions where applicable; use **`--yaml`** for machine-readable YAML (with **`--color`** / TTY auto for syntax-highlighted YAML)

This is less destructive than [`hydra gitops uninstall`](uninstall.md): the resources stay in the cluster and only the running workload instances are stopped or restored.

### Scale status output

`hydra gitops scale status` loads the full live cluster inventory from that shared unified model (same merge model as scale up/down), then for every scale target in the selected apps reports **two** dimensions:

**1. Sync state:**

- **`up`** — live object matches the rendered template (replica counts, DaemonSet `nodeSelector`, Job `suspend`, or custom `global.hydra.scale` paths)
- **`down`** — live object matches Hydra’s scaled-down shape (zero replicas, job suspended, DaemonSet disabled via the Hydra nodeSelector, or custom paths all zero)
- **`missing`** (text) / **`missing`** (YAML) — there is **no** live object for a **`batch/v1/Job`** scale target (common after a Job with **`spec.ttlSecondsAfterFinished`** finished and the TTL controller removed it). **Ready** is not evaluated for that row. Terminal color uses a light yellow / amber tone (not red). If the Job has **no** live object but **at least one** dependency edge classified as **out** (`refRole` **`produces`** or **`downstream`**) and **every** dependency row is **`up`** with **`ready`** (or no ready rule), the root row is shown as **`ok`** instead (green, same as **`up`**): the init/materialization outcome is treated as satisfied.
- **`ok`** (text and YAML) — only for **missing** Jobs that pass the rule above; otherwise not used as a sync state.
- **`out of sync`** (text) / **`out_of_sync`** (YAML) — anything else (including a missing live object when the above Job+TTL rule does not apply). If the template wants zero replicas and the cluster matches, the row is **`up`** (template wins when both shapes match).

**2. Ready state** (only when a readiness rule applies):

- **`ready`** / **`not_ready`** — derived from **`global.hydra.ready`**: each matching rule has a YAML list of CEL expressions; **all** must **pass** for **`ready`**; **any** failure (non-empty string, non-empty list of strings, or evaluation error) → **`not_ready`**.
- **`readyMessages`** (YAML) / **bullet lines** (default text) — when **`not_ready`**, Hydra lists the **reason strings** returned by the failing checks (and built-in defaults use human-readable English messages). List results from a single expression are **flattened** into multiple lines. Omitted when **`ready`** or when there is nothing to report. **Kubernetes Events** correlated to the workload appear only when **`global.hydra.ready`** (including built-in rules) returns them via CEL — for example through **`involvedObjectEvents(...)`** or by filtering **`clusterEntities()`** for `v1/Event` / `events.k8s.io/v1/Event`; Hydra does **not** run a separate post-pass Event list against the API for scale status.
- **Omitted** — if **no** rule’s predicate matches that entity, Hydra does **not** show a ready column/value and does **not** list that entity in **ready-specific** dependency output. This is intentional: ready is opt-in per entity via rules (including **built-in defaults** shipped with Hydra for standard scale kinds—see below).

**Ready dependencies** follow the **transitive** reference graph (same idea as [`hydra gitops inspect`](inspect.md) / [`hydra local inspect`](../local/inspect.md): paths through Secrets, operator CRs, and other intermediaries), but **only** entities that **also** have a matching ready rule appear in the ready-oriented dependency listing.

**Pods (`v1/Pod`):** Outgoing refs to Pods are included in each scale target’s **dependency rows** (direct edges and transitive reach, same hop cap as inspect). Sync state for a Pod row is **`up`** if the object exists in the live cluster and **`down`** if not. **Ready** uses the **built-in default** for Pods: **`ready`** when `status.phase` is **`Succeeded`**, or when phase is **`Running`** and the **`Ready`** condition is **`True`**; otherwise **`not_ready`** (including **`Failed`** and **`Pending`**). You can still override or extend behavior with **`global.hydra.ready`** rules keyed by predicate.

**Secrets and ConfigMaps (`v1/Secret`, `v1/ConfigMap`):** When they appear as **transitive** dependency rows (ref graph from the workload, same hop cap as inspect), Hydra applies a **built-in** rule: **`clusterEntities()`** must contain an entry with the same **`id`** as the dependency. **Sync** state is **`missing`** (amber in color mode) when there is **no** live object for that dependency; **ready** is not evaluated in that case (no duplicate **`missing`** line). When the object exists on the cluster, sync is **`up`** and **ready** is **`ready`** if the inventory check passes. **`hydra gitops scale up`** treats **`not_ready`** on these dependencies like other ready rules (waits until **`""`** / pass). Override or extend with **`global.hydra.ready`** using a more specific predicate if needed.

**Default stdout** is plain text with **ANSI colors** for state labels when color is enabled (TTY auto-detection, or `--color` / `--color-mode` like other commands). Each line lists the workload id, sync state, ready state when present, then dependencies on following indented lines (`optional` shown when the edge uses `optional:startup`). For **`not_ready`** rows with reasons, additional indented lines list each message prefixed with a hyphen and two leading spaces (same in color mode, with the message in red).

Dependency lines are ordered **`out`** first ( **`produces`** / **`downstream`** ), then **`in`** ( **`prerequisite`** ), then unclassified, each group sorted by id. The default text report may show a short **`in`** or **`out`** marker (light blue when ANSI color is enabled) before the dependency id: ref-parser **labels** on the direct edge when present (**`in`** for consumption labels such as volume/env/**`imagePullSecret`**, **`out`** for **`source`**), otherwise **GVK** fallbacks — **`in`** for **Secret** / **ConfigMap** and **`out`** for **ReplicaSet** / **Pod** (controller-managed children). The YAML report adds optional **`refRole`**: `prerequisite`, `produces` (label **`source`**), or `downstream` (ReplicaSet/Pod). Omitted when unknown (for example other kinds without labels).

With **`--yaml`**, stdout is a YAML document (`workloads`, nested `dependencies`) carrying these fields. Use **`--color`** (or TTY auto) to apply YAML syntax highlighting the same way as other Hydra YAML commands.

**Default scope:** Each scale-target block is **omitted** on its own when it needs no attention: either the root and **all** dependencies are fully healthy (root **`up`** or **`ok`**, **`ready`** when a rule applies), **or** the root is a **missing** **`batch/v1/Job`** with **at least one** dependency and **every** dependency row **`up`** and **`ready`**. Other workloads in the same app still appear. Use **`--all`** or **`-A`** to include omitted rows as well (same for **`--yaml`**). When the default **text** output would print **no** rows because **every** scale target in the selection was omitted this way, Hydra prints a single line instead: **all scale targets in the selection are up and ready (no issues found)** (green when ANSI color is enabled). **`--yaml`** is unchanged: an empty **`workloads`** list still means there was nothing to highlight in the filtered view, including the all-healthy case.

This command does not mutate the cluster. **`--no-cluster`** is not supported (a live API listing is required).

### Readiness configuration (`global.hydra.ready`)

Under **`global.hydra`** in Helm values, **`ready`** is a map of **named rules**. Each rule has:

- **`predicate`** — CEL expression; when **`true`**, the rule applies to that entity. Uses the **same** entity variables as ref-parser predicates and `--include` / `--exclude` (for example `gvk`, `ns`, `name`, `entity` for full object fields). See the CLI [CEL resource filters](../README.md#cel-resource-filters) overview and developer [CEL details](../../../develop/hydra-go/details/cel.md#global-hydra-ready-rules-predicate-and-cel-list).
- **`cel`** — list of CEL expressions; each must return **`null`** (omit), **`""`** or **`[]`** (pass), a **non-empty string**, or a **list of strings** (failure reasons). **All** must pass for **ready**. **`bool` is not accepted.**

If **no** rule matches, the entity is **not** part of ready display or ready gating.

**Merge:** Values follow the normal Hydra / Helm values hierarchy for `global.hydra` (same idea as `global.hydra.scale` and `global.hydra.refs`).

**Built-in defaults:** Hydra ships **default** ready rules for built-in scale kinds (Deployment, StatefulSet, ReplicaSet, DaemonSet, Job), for **`batch/v1/CronJob`** when it appears on the ref graph (**`spec.suspend`** must not be **true**), for **`v1/Secret`** and **`v1/ConfigMap`** on the scale dependency graph (live presence via **`clusterEntities()`**), and for custom targets declared in **`global.hydra.scale`**, so **`scale status`** keeps showing ready information for those workloads even when you add **no** custom `ready` block. Defaults mirror replica- or status-field checks consistent with scale-up polling (for example desired vs ready replicas for Deployments). Exact expressions are product-defined; see [developer values documentation](../../../develop/hydra-go/details/values.md#built-in-default-ready-rules).

**Example — `status.conditions` style readiness in CEL:** Kubernetes conditions are often modeled as “every condition `status` is `True`, and none is `False`”. You can express that with CEL on `entity.status.conditions` (field paths depend on your CRD). One illustrative pattern (split across the `cel` list as you prefer):

```yaml
global:
  hydra:
    ready:
      my-resource-conditions:
        predicate: 'gvk == "example.com/v1alpha1/MyResource"'
        cel:
          - 'size(entity.status.conditions) > 0 ? "" : "status.conditions is empty"'
          - '!entity.status.conditions.exists(c, c.status == "False") ? "" : "a condition has status False"'
          - '!entity.status.conditions.exists(c, c.status != "True" && c.status != "False") ? "" : "a condition has an unknown status"'
```

If Hydra later adds a **dedicated helper** for conditions, the manual will reference it; until then, prefer explicit CEL like the above.

**Results:** Each `cel` entry should return **`string`**, **`list(string)`**, or **`null`** as documented above. Evaluation **errors** surface as **`not_ready`** with a message in **`readyMessages`** (see developer docs).

**Live inventory helpers:** In cluster commands that merge rendered templates with live state, ready rules may call **`templateEntities()`**, **`clusterEntities()`**, **`entities()`**, their selector-object overloads such as **`clusterEntities({"namespace": ns, "gvk": "v1/Event"})`**, **`managedNamespaces()`**, and **`involvedObjectEvents(limit, kind, name, namespace)`** without extra API listing. See [developer CEL details](../../../develop/hydra-go/details/cel.md#global-hydra-ready-rules-predicate-and-cel-list).

### Pods after scale down

Workloads are scaled via Deployments, StatefulSets, DaemonSets, ReplicaSets, and custom targets from `global.hydra.scale`. **Pods** are a separate concern: some charts declare a standalone `Pod`; others exist only as children of a controller.

**After** template workloads and **cluster-only** workloads (see [Cluster-only workloads (scale down)](#cluster-only-workloads-scale-down)) are scaled down, Hydra refreshes the live Pod list when either workloads were mutated **or** remaining Pods need attention (for example app-associated Pods still **running** or **terminating**). Then:

- Deletes Pods that appear **directly** in your rendered manifests as `v1/Pod`.
- For Pods tied to your app **through `ownerReferences`** (directly or transitively—for example a Pod owned by a ReplicaSet that belongs to your Deployment), or owned by a workload (such as a StatefulSet) that Hydra associates with a template-anchored entity **via the same Hydra refs as cluster-only scale-down** (for example Pod → StatefulSet → `postgresql` CR ref): without `--force-scale-down`, Hydra logs **WARN** only (including Pods stuck in **Terminating** with a deletion timestamp). With `--force-scale-down`, Hydra **force-deletes** those Pods (`GracePeriodSeconds: 0`), in addition to the existing behavior of force-deleting workload Pods that stay stuck until the scale-down **timeout** expires.

If such app-associated Pods still need cleanup and you did **not** pass `--force-scale-down`, Hydra logs all related WARN lines first, then a **single** hint that you can re-run with `--force-scale-down` to delete or force-delete those Pods.

**After scale up** (planned): the same rule as for scale down applies—Pod reconciliation runs **only if** the scale-up path **actually changed the cluster** (for example a successful replica or DaemonSet patch). If nothing was mutated (already at desired state) or you use **`--dry-run`**, Hydra performs **no** real API creates for this Pod step; in dry-run, any create would be logged in the same `[dry-run]` style as workload patches, without touching the cluster.

When the mutation condition is met, Hydra can **create** missing **template** `v1/Pod` objects—Pods that exist as real manifest objects in your chart. It does **not** directly create Pods that are only supposed to appear as replicas created by a Deployment, StatefulSet, or operator; those are expected once the controller reconciles.

Details for implementers: [Cluster scale flow — Pod reconciliation](../../../develop/hydra-go/details/commands/deletion-and-topology/topology-and-scaling/cluster-scale-flow.md#pod-reconciliation-after-scale-planned).

### Operator-managed workloads

Some operators (for example the Prometheus Operator) create StatefulSets that are not part of the rendered Helm manifests. Because these StatefulSets are owned by a custom resource (CR) rather than by Helm, Hydra cannot scale them directly.

To handle this, declare the operator CRs as scale targets in `global.hydra.scale` in your app's Helm values. Each entry specifies a GVK and a JSON path to the replica field on the CR. When a StatefulSet's `ownerReference` points to a CR whose GVK is listed in `global.hydra.scale` and that CR is present in the rendered entity set, Hydra scales the CR instead of the StatefulSet.

There are **no** built-in entries inside **`global.hydra.scale` itself** — all operator CR scale targets must be declared explicitly there. Separately, **`global.hydra.ready`** has **built-in default rules** (see [Readiness configuration](#readiness-configuration-globalhydraready)) so standard workloads and those custom GVKs still get ready semantics in **`scale status`** and **`scale up`**. The same **`global.hydra.scale`** map is used for `hydra gitops apply` when **`--down-scaled`** and **`--scale-up`** are enabled (dependency-ordered scale-up after a down-scaled apply phase) and for the scale-down step at the start of [`hydra gitops uninstall`](uninstall.md).

### Cluster-only workloads (scale down)

After template workloads are scaled down, **`hydra gitops scale down`** runs an extra pass for **cluster-only** built-in workloads: Deployments, StatefulSets, ReplicaSets, and DaemonSets that exist on the API server but are **not** present as rendered template objects, when they are still linked to your app through transitive `ownerReferences` **or** through a **Hydra ref** from a template-anchored entity (template + live, same UID set as for owner-based linking) **to** that workload—using the same `From`/`To` normalization as scale dependencies, including `reverse` on the ref. This covers operator-created StatefulSets without Kubernetes `ownerReferences` when you declare an explicit ref (for example Zalando `postgresql` CR → operator StatefulSet). Those workloads are scaled to zero before pod reconciliation.

- **Without `--force-scale-down`:** Hydra waits up to **`--cluster-workload-timeout`** (default `1m`) for pods whose `ownerReferences` point directly at those workloads to terminate. If pods are still running when the timeout expires, Hydra logs a warning and **aborts** with an error.
- **With `--force-scale-down`:** No wait after this pass; remaining cleanup is handled by the existing pod reconciliation (including force-delete of app-associated pods). You **cannot** pass **`--cluster-workload-timeout`** and **`--force-scale-down`** together when both flags are explicitly set on the command line.

**Scale up:** Hydra does not add a separate cluster-only phase. When template workloads (including operator CRs from `global.hydra.scale`) are scaled back up, controllers are expected to recreate cluster-only children.

Details for implementers: [Cluster scale flow — cluster-only workload scale down](../../../develop/hydra-go/details/commands/deletion-and-topology/topology-and-scaling/cluster-scale-flow.md#cluster-only-workload-scale-down).

## Important ArgoCD Interaction

If ArgoCD auto-sync remains enabled, it may immediately reconcile workloads back to their desired replica counts after you scale them down. For planned maintenance, pair scale operations with [`hydra argocd`](../argocd/README.md):

1. `hydra argocd sync prevent ...`
2. `hydra gitops scale down ...`
3. Perform maintenance
4. `hydra gitops scale up ...`
5. `hydra argocd sync auto ...`

## When To Use It

Use `scale` for temporary operational changes:

- Maintenance windows
- Cost-saving shutdowns in non-critical environments
- Controlled restart sequences

Do not use `scale` when the goal is permanent removal. Use [`hydra gitops uninstall`](uninstall.md) for that.

## Arguments

| Argument                  | Description                                                                                |
| ------------------------- | ------------------------------------------------------------------------------------------ |
| `up`, `down`, or `status` | Scale direction, or read-only status report                                                |
| `appId`                   | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Description |
| --- | --- |
| `--hydra-context` | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--no-cache` | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--dry-run` / `-d` | Preview only: no scale patches or pod API writes; `[dry-run]` logs (not with `--no-cluster`). |
| `--no-cluster` | Render and resolve apps only; skip cluster connection and all scale API calls (not for `status`). |
| `--scale-timeout` | Timeout waiting for pods to reach the desired state (e.g. `10m`) |
| `--crd-timeout` | Timeout for CRD establishment (e.g. `60s`) |
| `--force-scale-down` | Scale down: stuck Pods on timeout; force-deletes app-associated Pods; skips cluster-only pod wait. |
| `--cluster-workload-timeout` | After cluster-only scale (default `1m`), wait for pods. Not with `--force-scale-down` if both set. |
| `--exclude-app` | Glob pattern to exclude applications (repeatable) |
| `--include` / `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` / `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--yaml` | **`scale status` only:** emit YAML instead of the default colored text report |

### Dry-run

With `--dry-run`, Hydra still connects to the cluster (unless you combine with other offline-oriented workflows), lists live state, and walks the same scale-up or scale-down plan as a real run, but **does not** apply patches, delete Pods, or create template Pods. Workload steps log with a `[dry-run]` prefix; pod reconciliation follows the same rules as a normal run but only emits `[dry-run]` lines for actions that would mutate the cluster. Use this to preview what would change before executing a real scale.

## Examples

```bash
# Scale down all workloads for an app (replicas → 0)
hydra gitops scale down prod.apps.my-service

# Scale down all apps on a cluster
hydra gitops scale down prod.apps.*

# Recommended maintenance flow with ArgoCD freeze
hydra argocd sync prevent prod.apps.*
hydra gitops scale down prod.apps.*
# ... perform maintenance ...
hydra gitops scale up prod.apps.*
hydra argocd sync auto prod.apps.*

# Force-kill stuck pods during scale down
hydra gitops scale down prod.apps.my-service --force-scale-down

# Scale back up (restores configured replica count)
hydra gitops scale up prod.apps.*

# Scale up with extended timeout for slow-starting services
hydra gitops scale up prod.apps.* --scale-timeout 15m

# Scale only Deployments (skip StatefulSets)
hydra gitops scale down prod.apps.* --include 'kind == "Deployment"'

# Preview scale-down without mutating the cluster
hydra gitops scale down prod.apps.my-service --dry-run

# Read-only: default colored text (workload state + workload dependencies)
hydra gitops scale status prod.apps.my-service

# Machine-readable YAML; add --color for highlighted YAML on a TTY
hydra gitops scale status prod.apps.my-service --yaml
```

## See Also

- [`hydra gitops uninstall`](uninstall.md) — fully remove resources (more destructive)
- [`hydra argocd`](../argocd/README.md) — prevent ArgoCD from re-scaling during maintenance
