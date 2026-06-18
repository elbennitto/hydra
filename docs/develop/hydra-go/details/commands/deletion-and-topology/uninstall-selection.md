# Deletion and Topology: Uninstall Selection

This page covers uninstall ref tags, ref-group configuration, and the entity selection helpers used before deletion begins.

Back to [Deletion and Topology](../deletion-and-topology.md).

## Uninstall via Ref Tags

During `hydra gitops uninstall`, Hydra uses ref tags to decide what to do with resources not directly managed by ArgoCD. App-defined ref-parsers with uninstall tags (`uninstall`, `uninstall-safe`, `uninstall-force`) and a finalizer list in `global.hydra` control this behavior.

See [references.md — Tags, Desc, and Label](../../references.md#tags-desc-and-label) for the full tag documentation and [references.md — App-Defined Ref-Parsers](../../references.md#app-defined-ref-parsers) for the YAML format.

### Overview

```yaml
global:
  hydra:
    refs:
      group-name:
        tag: [uninstall] # or [uninstall-safe], [uninstall-force]
        desc: "Optional description"
        ref-parsers:
          - predicate: "<CEL expression>"
            pick:
              - "<CEL expression returning RefDefinition>"
    uninstall-finalizer:
      - "<finalizer name>"
```

| Tag                   | Effect                                                                                                                         | Typical use case                                                                         |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| **`uninstall`**       | If the source entity is deleted, the target entity may also be deleted. Only within the defining app.                          | Operator-created ephemeral resources (Pods, Services, StatefulSets, Events)              |
| **`uninstall-safe`**  | Works across apps. Untracked resources can be marked as "safe for uninstallation" when another app's source entity is present. | Reports, events, or secrets created by App A that reference resources in App B           |
| **`uninstall-force`** | Target resource is important; deletion must be explicitly allowed with `--force`.                                              | PVCs, cluster-created secrets that should be backed up (e.g. Let's Encrypt certificates) |

| Block                   | YAML key                           | Effect                                                                           |
| ----------------------- | ---------------------------------- | -------------------------------------------------------------------------------- |
| **uninstall-finalizer** | `global.hydra.uninstall-finalizer` | Specified finalizers are removed cluster-wide from ALL resources that carry them |

### Namespace set for leftovers and uninstall-safe

`handleLeftovers`, `handleForceLeftovers`, and uninstall-safe CEL (`ns in namespaces`) use **`UninstallLeftoverNamespaces`**: the union of **ExclusiveNamespaces** and **every namespace that appears in the selected apps’ rendered templates** (`GroupNamespacesByApp`). That keeps shared namespaces (for example `demo`) in scope for scanning.

### Template ownership and uninstall-force assignment

Before uninstall continues, **`ValidateNoDuplicateTemplateResourceIds`** runs on **per-app standalone renders** (`RenderClusterEachAppSeparate`): the same `types.Id` must not appear in more than one app’s template output — otherwise uninstall aborts with **`ErrUninstallDuplicateTemplateResource**`.

For cluster **leftovers** (objects in those namespaces not already in the uninstall batch), **`ClassifyLeftoversUninstallForce`** evaluates **`uninstall-force`** ref predicates **per app** (each app uses its own rendered inventory for CEL). For each leftover:

- **Zero** apps match → **warning** (logged; not deleted unless `--force-all` merges warn-listed entities).
- **Exactly one** app matches and that app is **in the uninstall selection** → **force-deletable** (same `--force` / `--keep` / default abort behavior as before).
- **Exactly one** app matches and that app is **not** selected → **ignored** for this uninstall (belongs to another app).
- **More than one** app matches → **`ErrUninstallAmbiguousLeftoverRef`** (abort; tighten predicates).

The old “untracked = no predicate match → always abort unless `--force-all`” behavior is replaced: non-matching leftovers only **warn** unless you use **`--force-all`**, which also merges those warn-listed resources into the deletion set.

### Global hydra presets — builtin CEL cluster defaults

Hydra ships **builtin** YAML presets under `core/hydra/embed/presets/` (`coredns`, `kubernetes`, `flannel`, `canal`, `kubermatic`, `syseleven`, `metakube`, `syseleven-node-problem-detector`, `quobyte`, `cloudinit`, `cinder`). The **kubernetes** preset holds upstream bootstrap RBAC/core objects in **`ids`** groups (**`bootstrap-audit`**, **`bootstrap-audit-m{N}`**, **`default-namespace-injected`**, …); **`rbac-cluster-roles`** / **`rbac-cluster-role-bindings`** CEL lines are **synthesized** from those ids at load when not overridden in YAML. **Coredns**, **flannel**, **canal**, **kubermatic**, **cloudinit**, **cinder**, **syseleven-node-problem-detector**, and **quobyte** contribute **`ids`** and/or optional **`cel`** via their named predicate groups (for example **kubermatic** includes Cluster API **`MachineDeployment`** in **`kube-system`** via CEL). **Syseleven** only lists **`activates`** (no `predicates`). **Metakube** is a bundle preset (no resource predicates by default): it only lists **`activates`** for **cloudinit**, **cinder**, and **kubermatic**. Together they extend blanket audit coverage when each preset is enabled. Helm and Hydra ConfigMap documents merge into **`global.hydra.presets`** the same way as other `global.hydra` blocks (per-app order, then global docs; nested **`predicates.<name>`** deep-merge: **`enabled`** toggles, **`cel`** / **`ids`** replace the corresponding list for that name when set).

Builtin YAML and merged **`global.hydra.presets.<id>`** blocks may set **`activates`**: a list of other top-level preset ids that Hydra **forces on** whenever that preset is effectively enabled, applied in a transitive closure after **`enabled`** merges. Builtin **syseleven** activates **metakube**, **syseleven-node-problem-detector**, and **quobyte**; **metakube** activates **cloudinit**, **cinder**, and **kubermatic**. Helm/ConfigMap **`activates`** for a preset **replace** the builtin list when set. The merged **`activates`** graph must be **acyclic**; otherwise Hydra aborts with a configuration error. If a target preset was merged with **`enabled: false`**, an enabled activator still turns it **on**; to keep bundled presets off, disable the activating preset (e.g. **syseleven**).

Each named predicate may set **`cel`**, **`ids`**, or both. **OR** within **`cel`**, **OR** between **`cel`** and **`ids`** when both are set (after optional **`kubernetesMinorMin`** / **`kubernetesMinorMax`** gating on that group). **OR** across names, **OR** across enabled top-level presets. CEL uses the same entity variables as ref/uninstall predicates on cluster entities (**`gvk`**, **`ns`**, **`name`**, …) with **`cel.NewEnvWithEntityInventory(renderedAllApps)`** (full-cluster template inventory).

**`ids`** are explicit Hydra resource id strings. Only **`ids`** contribute to the **finite expected-id union** used by the cluster-review blanket bootstrap audit (missing upstream defaults). CEL matches at runtime but does **not** add ids to that audit list—so prefer **`ids`** when a resource should be covered by that audit. **`cel`** remains appropriate for broad selection or when audit coverage is not required.

The apiserver-injected **ServiceAccount `default`** and **ConfigMap `kube-root-ca.crt`** in namespace **`default`** are part of the builtin **`kubernetes`** preset via explicit **`ids`** (not the **`coredns`** / **`flannel`** / **`canal`** / **`kubermatic`** / **`syseleven`** / **`metakube`** / **`syseleven-node-problem-detector`** / **`quobyte`** / **`cloudinit`** / **`cinder`** presets).

**Uninstall (`handleForceLeftovers`):** After `ClassifyLeftoversUninstallForce`, **warn-listed** leftovers (no `uninstall-force` match in any app) are **removed** from the warn list when they match an **enabled** preset — they are treated as explained infrastructure noise, not “untracked”. Leftovers that **do** match an app’s `uninstall-force` rules (including “matched another app → ignored”) are **not** on that warn list; if such an entity **also** matches an active preset, Hydra logs an extra **warning** so operators review preset coverage versus real app ownership.

**Cluster ref ownership review (`AppendRefOwnershipReviewFindings`, `hydra gitops review cluster`):** For cluster-only resources, if a resource matches an active preset and **no** app assignment applies, the review **does not** emit **`ref ownership: cluster-only resource has no Hydra app assignment`** for that id. If the resource **has** a unique app assignment **and** matches an active preset, the review emits a separate finding: **`ref ownership: cluster-only resource matches cluster defaults preset(s) and a Hydra app assignment`**.

**Diagnostics:** **`hydra gitops system <cluster>`** prints merged preset enablement, for each **`ids`** entry whether the id appears in the live inventory (after minor gating), and for each CEL line the live **`ListClusterAll`** entity ids that match (read-only).

### PersistentVolume follow (built-in PVC→PV ref)

**PersistentVolume** is cluster-scoped, so it is not selected by namespace-scoped leftover passes. After the uninstall entity set is finalized (including PVCs added via `--force` / `--force-all`), Hydra merges **PersistentVolume** objects referenced by the built-in ref edge from **PersistentVolumeClaim** (`spec.volumeName` → PV). Refs are taken from the same cluster-inventory ref pipeline as the cluster ref tree (`commands.ClusterInventoryRefs`); merge happens in `commands.MergePersistentVolumesBoundToUninstallClaims` before the uninstall preview and deletion phases.

### Decision flow for unmanaged resources

```text
Resource in namespace selected for uninstall
  │
  ├── Matched by ref with tag "uninstall"?
  │   YES → silently added to deletion set
  │
  ├── Matched by ref with tag "uninstall-safe" (+ namespace check)?
  │   YES → silently added to deletion set
  │
  ├── uninstall-force: per-app predicates classify the leftover
  │   → 0 apps: warning only (optional --force-all merges into delete set)
  │   → 1 app (selected): force-deletable leftover → --force / --keep / abort
  │   → 1 app (not selected): ignored here
  │   → 2+ apps: ErrUninstallAmbiguousLeftoverRef (abort)
  │
  └── (legacy "untracked" abort for any non-matching leftover is removed)
```

### tag: uninstall

Refs with the `uninstall` tag mark target entities for automatic deletion when the source entity's app is uninstalled. This only applies within the app that defined the ref-parser.

**When to use:** For resources dynamically created by operators that are ephemeral and will be recreated if the operator is reinstalled — Services, Pods, StatefulSets, EndpointSlices, Events, Leases.

**Example:**

```yaml
global:
  hydra:
    refs:
      operator-resources:
        tag: [uninstall]
        desc: "Operator-created ephemeral resources for PostgreSQL"
        ref-parsers:
          - predicate: 'id.startsWith("v1/Service/demo/psql-demo") || id.startsWith("v1/Pod/demo/psql-demo")'
            pick:
              - '[refBuilder().outgoing(ref("provider", "postgres"))]'
```

### tag: uninstall-safe

Refs with the `uninstall-safe` tag work across apps. App A defines the rule. When the user installs or uninstalls App B, which has a source entity of the rule, untracked resources can be marked as "safe for uninstallation".

**When to use:** For resources managed by a different app that can be safely deleted — they will be recreated by the managing app when needed. Examples: untracked reports or events created by App A that reference a resource in App B.

**Example:**

```yaml
global:
  hydra:
    refs:
      cross-app-secrets:
        tag: [kyverno, uninstall-safe]
        desc: "Kyverno-managed image pull secrets recreated on demand"
        ref-parsers:
          - predicate: 'gvk == "v1/Secret" && name == "image-pull-secret" && clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "kyverno"'
            pick:
              - '[refBuilder().outgoing(ref("provider", "kyverno"))]'
```

**Built-in:** `PodMetrics` resources (`metrics.k8s.io/v1beta1/PodMetrics`) are always treated as safe for uninstallation.

### tag: uninstall-force

Refs with the `uninstall-force` tag mark target resources that belong to the source resource but are important. Deletion must be explicitly allowed with `--force`.

**When to use:** For persistent data that operators create dynamically and that should normally survive an uninstall — Secrets with database credentials, PersistentVolumeClaims with data, Let's Encrypt certificates managed by cert-manager.

**Example:**

```yaml
global:
  hydra:
    refs:
      persistent-data:
        tag: [uninstall-force]
        desc: "Persistent data created by the PostgreSQL operator"
        ref-parsers:
          - predicate: 'id.startsWith("v1/PersistentVolumeClaim/demo/pgdata-psql-demo")'
            pick:
              - '[refBuilder().outgoing(ref("provider", "postgres"))]'
```

**CLI flags:**

| Flag          | Behavior                                                                                                  |
| ------------- | --------------------------------------------------------------------------------------------------------- |
| `--force`     | Force-deletable resources are added to the deletion set                                                   |
| `--keep`      | Force-deletable resources are ignored, uninstall proceeds                                                 |
| `--force-all` | Force-deletable AND untracked resources are all added to the deletion set                                 |
| _(neither)_   | Uninstall aborts with a message listing the resources and suggesting `--force` / `--keep` / `--force-all` |

### uninstall-finalizer

The `global.hydra.uninstall-finalizer` block specifies finalizer names (plain strings, no CEL). When an app is uninstalled, Hydra removes these finalizers from ALL resources in the ENTIRE cluster (cluster-wide, not scoped to the uninstall namespaces). Only the listed finalizers are removed; other finalizers on the same resource are preserved.

**When to use:** For finalizers that the uninstalled app attaches to resources in other namespaces. Without this step, those resources (and their namespaces) cannot be deleted because Kubernetes blocks deletion while a finalizer is present and the controller that would remove it is gone.

**Example:**

```yaml
global:
  hydra:
    refs:
      argocd:
        tag: [uninstall]
        ref-parsers:
          - predicate: 'group == "argoproj.io"'
            pick:
              - '[refBuilder().outgoing(ref("provider", "argocd"))]'
    uninstall-finalizer:
      - "argocd.argoproj.io/hook-finalizer"
```

**Scope:** Cluster-wide — all namespaces and cluster-scoped resources are scanned.

**Dry-run:** When `--dry-run` is active, matching finalizer removals are logged but not executed.

**Source:** Finalizer names are loaded from the apps being uninstalled (`appIds`) via `HydraAppUninstallFinalizers()` using `HelmNetworkModeOffline`.

#### When are finalizers removed, and when not?

Hydra only removes finalizers from resources when the app that owns the finalizer controller is itself being uninstalled. The `uninstall-finalizer` list is scoped to the apps selected for uninstallation (`appIds`). If an app is not part of the current uninstall operation, its `uninstall-finalizer` entries are not loaded and Hydra does not touch those finalizers.

##### Decision flow

```text
Is the app that owns the finalizer controller being uninstalled?
  │
  ├── NO  → The controller is still running.
  │         The controller handles its own finalizers during normal
  │         Kubernetes resource lifecycle. Hydra does NOT intervene.
  │
  └── YES → The controller will be gone after uninstall.
            Hydra loads the app's uninstall-finalizer list and
            removes the listed finalizers from ALL cluster resources
            BEFORE deleting the app's own resources.
            Without this step, Kubernetes would block deletion of
            resources carrying the finalizer indefinitely, because
            no controller exists to reconcile and remove it.
```

##### Example: ArgoCD finalizer on a root Application resource

Setup: The app `in-cluster.argocd` declares `argocd.argoproj.io/hook-finalizer` in its `uninstall-finalizer` list. A root Application resource in another namespace carries this ArgoCD finalizer.

| Scenario                                 | ArgoCD uninstalled? | Who removes the finalizer?  | Explanation                                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------------------- | ------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **(a)** ArgoCD stays running             | No                  | **ArgoCD** (the controller) | ArgoCD is not part of `appIds`, so its `uninstall-finalizer` entries are not loaded. The ArgoCD controller is alive and handles finalizer removal itself during normal reconciliation.                                                                                                                                                                               |
| **(b)** ArgoCD is also being uninstalled | Yes                 | **Hydra**                   | ArgoCD is part of `appIds`, so Hydra loads `argocd.argoproj.io/hook-finalizer` from the uninstall-finalizer config. Before deleting ArgoCD's own resources, Hydra patches all cluster resources that carry this finalizer to remove it. This prevents orphaned finalizers that would block deletion of namespaces and resources after the ArgoCD controller is gone. |

### enabled field

Ref groups can be disabled by setting `enabled: false`. This is useful for parent chart overrides where a child chart defines ref groups that should not apply:

```yaml
global:
  hydra:
    refs:
      some-group:
        enabled: false
```

When `enabled` is omitted or set to `true`, the group is active. The `IsEnabled()` method on `HydraRefGroup` returns `true` if `Enabled` is nil or `true`.

### Types

Defined in `core/types/hydra.go`:

```go
type HydraRefGroup struct {
    Tag        []string         `yaml:"tag"`
    Desc       string           `yaml:"desc,omitempty"`
    Label      string           `yaml:"label,omitempty"`
    Enabled    *bool            `yaml:"enabled,omitempty"`
    RefParsers []HydraRefParser `yaml:"ref-parsers"`
}

type HydraRefParser struct {
    Predicate string         `yaml:"predicate"`
    Pick      []HydraRefPick `yaml:"pick"`
}

type HydraRefPick struct {
    Cel string `yaml:"cel"`
}
```

Methods on `HydraRefGroup`:

| Method             | Description                                  |
| ------------------ | -------------------------------------------- |
| `IsEnabled() bool` | Returns `true` if `Enabled` is nil or `true` |

Fields on `HydraValues`:

```go
Refs               map[string]HydraRefGroup     `yaml:"refs"`
UninstallFinalizer []string                     `yaml:"uninstall-finalizer"`
Scale              map[string]HydraScaleGroup   `yaml:"scale"`
```

`HydraScaleGroup` (in `core/types/hydra.go`):

```go
type HydraScaleGroup struct {
    GVK             string   `yaml:"gvk"`
    ReplicaPaths    []string `yaml:"replicaPaths"`
    StatusReadyPath string   `yaml:"statusReadyPath,omitempty"`
}
```

### Values loading functions

In `core/hydra/hydra_values.go`:

| Function                                               | Purpose                           | Collects from | Returns                                     |
| ------------------------------------------------------ | --------------------------------- | ------------- | ------------------------------------------- |
| `HydraAppRefGroups(cluster, appIds)`                   | All enabled ref groups            | selected apps | `map[string][]HydraRefGroup`                |
| `HydraAppUninstallFinalizers(cluster, appIds)`         | Finalizer names                   | selected apps | `[]string` (deduplicated)                   |
| `HydraAppScaleWorkloads(cluster, appIds, networkMode)` | Custom scale workload definitions | selected apps | `map[types.GVKString]types.HydraScaleGroup` |

For the detailed `HydraAppScaleWorkloads` behavior and scale-target test coverage, see [Scale Targets](topology-and-scaling/scale-targets.md#hydraappscaleworkloads).

## Selection / Marking Commands

These functions mark entities for operations like uninstall. Selection uses the `KeySelected` flag on entities.

### Uninstall predicate compilation scope

Cluster uninstall compiles app-defined CEL from ref groups in three places; together they match the **uninstall predicate compilation scope** defined in [CEL details — uninstall predicate compilation scope](../../cel.md#uninstall-predicate-compilation-scope):

1. **`uninstall` and `backup`** — `MarkAsSelectedByUninstallPredicates` collects uninstall predicates and backup predicates in one marking pass.
2. **`uninstall-safe`** — `MarkAsSelectedBySafeForUninstallationPredicates` (only in namespaces where every stakeholder app is in the selected uninstall set; see **Uninstall stakeholders** below).
3. **`uninstall-force`** — leftover classification in `handleForceLeftovers` (force vs warn separation).

Before merge, cluster inventory in uninstall namespaces is assigned to **exactly one** app using `AssignClusterEntitiesToAtMostOneAppByRefs` and **`reconcileClusterOwnership`**: **standalone template id wins**. Ownership ref predicates are taken from enabled ref groups tagged **`uninstall`**, **`uninstall-safe`**, **`uninstall-force`**, and/or **`backup`** (same set as `HydraAppAllRefOwnershipPredicates`, plus clone-derived predicates; **`uninstall-safe` alone is enough to participate in this pass**). Per cluster object, `AssignClusterEntitiesToAtMostOneAppByRefs` chooses **`HydraAppAllRefOwnershipPredicates` with `includeRuntimeTaggedGroups: false`** when the object’s `types.Id` appears in any per-app standalone render (`RenderClusterEachAppSeparate`), and **`true`** when the id appears in **no** template — so **`runtime`**-tagged groups apply **only** to cluster-only ids (same split as **`hydra gitops review`** live ownership). **`hydra local review`** always passes `false` (no **`runtime`** groups). If the cluster object’s id appears in a standalone render, that app owns the resource; non-runtime predicates may still match that same app, but matching **any other** app aborts with `ErrUninstallRefOwnershipConflictsWithTemplate`. If the id appears in **no** template, ref-only rules apply: zero matches → `ErrUninstallUnassignedClusterResource`; more than one app → `ErrUninstallAmbiguousRefOwnership`.

For all of the above, if a predicate references **`managedNamespaces()`** or other entity-inventory helpers, compilation must use **`cel.NewEnvWithEntityInventory(rendered)`** with **`rendered`** equal to the same template entity set used to collect predicates for that helper. The per-function notes below apply the same contract.

### MarkAsSelectedArgoCdManagedResources

```go
func MarkAsSelectedArgoCdManagedResources(l log.Logger, entities entity.Entities, key types.EntityKeyUnstructured, appIds sets.Set[types.AppId]) (entity.Entities, error)
```

Marks entities that are managed by ArgoCD for specific apps. Detection uses the `argocd.argoproj.io/tracking-id` annotation:

```text
Annotation format: {appId}:{group}/{kind}:{namespace}/{name}
```

Entities whose tracking-id matches one of the given appIds are marked as selected.

### MarkAsSelectedPartOf

```go
func MarkAsSelectedPartOf(l log.Logger, entities entity.Entities, key types.EntityKeyUnstructured, appIds sets.Set[types.AppId]) (entity.Entities, error)
```

Marks entities with `app.kubernetes.io/part-of` labels matching the given apps.

### MarkAsSelectedByUninstallPredicates

```go
func MarkAsSelectedByUninstallPredicates(cluster *hydra.Cluster, entities entity.Entities, appIds sets.Set[types.AppId]) (entity.Entities, error)
```

Marks entities matching app-defined ref groups tagged `uninstall`, and also predicates from ref groups tagged `backup` in the same pass (backup implies uninstall). Predicates come from `HydraAppUninstallPredicates` and `HydraAppBackupPredicates`.

**CEL compilation contract:** See **Uninstall predicate compilation scope** (subsection above). Helpers must compile with **`cel.NewEnvWithEntityInventory(rendered)`** when predicates may reference **`managedNamespaces()`** or other inventory functions.

### Uninstall stakeholders (template ∪ ref-assigned cluster)

`TemplateAppsByNamespace`, `AssignClusterEntitiesToAtMostOneAppByRefs`, and `StakeholderAppsByNamespace` build per-namespace sets of Hydra apps that still have **template** entities and/or **cluster** entities (template ownership is authoritative; refs must agree with the template owner when both apply). `AssignClusterEntitiesToAtMostOneAppByRefs` first applies template ids and ref-parser matches, then **`expandAssignmentByOwnerRefs`** walks **`metadata.ownerReferences`** within the same cluster inventory: any object whose direct owner (by UID) is already assigned inherits that app, repeated to a fixpoint so chains such as Pod → ReplicaSet → Deployment are covered when the Deployment is assigned. **`hydra gitops uninstall`** does **not** abort based on a separate “unassigned inventory” list; objects without assignment simply have no owning app in the computed map—cover them with **refs**, templates, **`ownerNamespaces`**, sole-template-namespace assignment, or transitive **`metadata.ownerReferences`** as appropriate. `NamespacesAllowingUninstallSafe` keeps only namespaces whose stakeholder set is a subset of the selected uninstall apps. That set is intersected with leftover uninstall namespaces and passed to **`MarkAsSelectedBySafeForUninstallationPredicates`** as **`safeNamespaces`**, so shared namespaces do not apply **`uninstall-safe`** until every stakeholder app is selected for uninstall.

**`hydra gitops review` ref ownership** (`AppendRefOwnershipReviewFindings` / `refOwnershipAppendFindings`) evaluates template ids with non-runtime ownership predicates, then iterates live cluster entities using **non-runtime** predicates when the id is template-mapped and **full** predicates (including **`runtime`**) when the id is cluster-only, then **`expandAssignmentByOwnerRefsSoft`** (same propagation as uninstall; ambiguous owner chains become review findings instead of aborting). Cluster-only resources that still have no app in that combined assignment and whose workload namespace lies in **`UninstallLeftoverNamespaces(exclusive, renderedAllApps)`** may be reported as **`ref ownership: cluster-only resource has no Hydra app assignment`** **only** when **`hydra gitops review cluster`** runs with `reportUnassignedClusterOnlyResources` enabled; **`hydra gitops review app`** passes `false` and skips that finding class. That unassigned pass omits objects that have **`metadata.ownerReferences`** to another entity in the same live inventory so only ownership **roots** are reported.

### MarkAsSelectedBySafeForUninstallationPredicates

```go
func MarkAsSelectedBySafeForUninstallationPredicates(cluster *hydra.Cluster, entities entity.Entities, allAppIds sets.Set[types.AppId], safeNamespaces sets.Set[types.Namespace], renderedAllApps entity.Entities) (entity.Entities, error)
```

Marks entities matching app-defined ref groups tagged `uninstall-safe` (`HydraAppUninstallSafePredicates` across **all** cluster apps), scoped with **`ns in safeNamespaces`** where **`safeNamespaces`** is computed from stakeholders (see above). The CEL environment includes **`SetSupport("namespaces", safeNamespaces)`** plus **`NewEnvWithEntityInventory(renderedAllApps)`**; app-defined predicates that use **`managedNamespaces()`** or **`templateEntities(...)`** follow the same render contract as in **Uninstall predicate compilation scope** (subsection above).

### Leftover force classification

Cluster entities remaining after the main uninstall selection are classified with **`ClassifyLeftoversUninstallForce`** (uninstall-force ref predicates only). Resources matching **zero** apps are reported as warnings and, together with **`resolveForceLeftovers`**, cause an **`ErrAborted`** unless **`--force-all`** is used.

### SeparateUninstallForceLeftovers

```go
func SeparateUninstallForceLeftovers(cluster *hydra.Cluster, leftovers entity.Entities, appIds sets.Set[types.AppId]) (forceLeftovers, untrackedLeftovers entity.Entities, error)
```

Takes leftover entities and separates them into force-deletable vs untracked using app-defined ref groups tagged `uninstall-force` (`HydraAppUninstallForcePredicates`). **CEL compilation contract:** see **Uninstall predicate compilation scope** (subsection above).
