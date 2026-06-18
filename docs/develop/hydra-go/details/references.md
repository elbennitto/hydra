# Reference Discovery Architecture

## Overview

References represent dependencies between Kubernetes entities (e.g. a Deployment referencing a ServiceAccount, a Service selecting a Deployment). Hydra discovers references automatically using CEL-based ref-parsers that analyze entity specifications and extract relationship endpoints.

**Source files:** `core/references/refs.go`, `core/references/ref-parsers/*.yaml`

**Cloning:** Manifest duplication of rendered objects into other namespaces is **not** done via refs; it uses **`global.hydra.clones`** (see [clones.md](clones.md)).

## Data Model

### Reference

```go
type Ref struct {
    RefType      RefType         // "direct", "indirect", "regarding" (and rarely "runtime" in legacy paths)
    EndpointType RefEndpointType // Endpoint type (e.g. "id")
    From         Id              // Source entity ID
    To           Id              // Target entity ID
    Labels       []string        // Relationship labels (e.g. volume, clone-source, namespace); primarily visual in UI
    Tags         []string        // Semantic tags (e.g. "optional:ref", uninstall markers); optional-edge and uninstall semantics
    Attributes   []RefAttribute  // origin:generated job|controller, origin:app/workload, explicit keys, etc.
    Reverse      bool            // When true, consumers treat dependency direction as reversed (UI + ordering/grouping); From/To unchanged
}
```

Human-readable **`desc`** lives on **`RefDefinition`** (parser output) and is merged into refs only where consumers copy parser metadata; the serialized **`Ref`** type has no `desc` field.

### Ref types (`RefType`)

| Type | Typical source | Ordering / review | Notes |
| ---- | -------------- | ----------------- | ----- |
| **`direct`** | Embedded or app ref-parsers | Yes | Default for manifest-derived edges |
| **`indirect`** | `ResolveTransitiveWorkloadDeps` and similar | Yes | Synthetic workload→workload via intermediaries |
| **`regarding`** | `events.k8s.io/v1/Event` parser (`.refType('regarding')`) | No | Informational Event→subject association; used by workload closure / preset matching, not apply topo |
| **`runtime`** | Reserved in `types.Ref` | No | Uninstall **tag** `runtime` is separate; materialization uses **`origin:generated`**, not this type |

End-user summary: [manual refs: Ref Types](../../../manual/refs/ref-types.md).

### Reference Definition

```go
type RefDefinition struct {
    Owner     Id           // Entity that owns this reference definition
    Type      RefType      // Reference type ("direct" or "indirect")
    Direction RefDirection // "incoming" or "outgoing"
    Endpoint  RefEndpoint  // Target endpoint (ID or custom ref)
    Label     string       // Relationship label
    Tags      []string     // Semantic tags
    Desc      string       // Human-readable description
    Attributes []RefAttribute // Structured repeated relation metadata
    Reverse   bool         // Whether direction is reversed
}

type RefEndpoint struct {
    Type  RefEndpointType  // "id", "provider", or custom type (e.g. "crd", "service")
    Value string           // The endpoint value (entity ID for type "id", app tag for type "provider", match value for custom types)
}

type RefDirection string
const (
    RefDirectionIncoming RefDirection = "incoming"  // Points toward the owner
    RefDirectionOutgoing RefDirection = "outgoing"  // Points away from the owner
)

type RefAttribute struct {
    Type  string // e.g. "key" or "origin:app"
    Value string // e.g. "SPRING_PROFILE" or "in-cluster.cluster-infra.kyverno"
}
```

`RefEndpoint` provides an `Id()` method that extracts the entity ID when `Type == "id"`.

## Semantic roles of labels, tags, attributes, and reverse

These fields overlap in the YAML schema but serve different purposes; mixing them makes review output and the graph harder to reason about.

| Field | Role | Typical content |
| ----- | ---- | --------------- |
| **`label`** | **Display** — short relationship name in the UI/graph (one primary label per `RefDefinition`; merged refs may carry multiple labels). | `volume`, `subject`, `serviceAccount`, `clone-source` |
| **`tag`** | **Behavior** — flags that change how Hydra treats the edge (uninstall rules, optional refs, startup ordering, backup markers, etc.). | `optional:ref`, `uninstall`, `optional:startup` |
| **`attributes`** | **Structured metadata** — typed key/value pairs for provenance, materialization policy, and explicit resource keys. | `origin:generated`, `origin:app`, `origin:workload`, `origin:owner`, `origin:source` (`template` / `cluster` / `test`), `key` |
| **`reverse`** | **Direction interpretation** — Does **not** change the stored **`From`/`To`** pair, but tells consumers to treat the edge as **reversed**: both **visual** arrow direction in the UI **and** **logical** dependency direction wherever Hydra interprets “depends on” (for example **apply ordering** / `BuildDependencyGraph`, **transitive workload resolution**, and **grouping** that follows ref direction). See [Reverse References](#reverse-references) and [Reverse Flag](#reverse-flag) under apply ordering. | `true` / `false` |

**Label naming convention** — Choose names that describe the **relationship** or the **target’s role** in that relationship, and keep them **consistent** across parsers (for example always `subject` for RoleBinding→ServiceAccount subject edges). Avoid misleading labels that suggest the wrong participant (for example naming an edge after a field name or a casual synonym when the graph role is different).

## Reference vocabulary (hydra-go)

This section is the **single source of truth** for **labels**, **tags**, and **attribute type names** shipped in **hydra-go** (embedded ref-parser YAML under `core/references/ref-parsers/` and Go constants in `core/types/refs.go`). When you add or rename a built-in label, tag, or attribute type, **update this section** together with the code or YAML.

**Scope:** Values that appear only in a **specific chart or app** (for example a concrete `origin:app` value or a particular `ref("provider", …)` name) are **not** catalogued here. **Generic patterns** that charts use with `global.hydra.refs` are summarized under [Generic patterns from chart configuration](#generic-patterns-from-chart-configuration).

### Labels (embedded parsers)

Relationship labels for **display** in the graph (short names; merged when multiple parsers contribute the same edge):

| Label | Typical meaning |
| ----- | --------------- |
| `crd` | Custom resource instance → its `CustomResourceDefinition` |
| `imagePullSecret` | Workload → `Secret` (pull secrets) |
| `serviceAccount` | Workload → `ServiceAccount` |
| `secret` | `ServiceAccount` → pulled `Secret` |
| `volume` | Workload → `ConfigMap` / `Secret` / PVC (volumes) |
| `volumeClaimTemplate storageClass` | `StatefulSet` → `StorageClass` (volume claim templates) |
| `volumeClaimTemplate` | `StatefulSet` → `PersistentVolumeClaim` expected from `volumeClaimTemplates` (controller-materialized PVCs; see [StatefulSet volume claim templates](#statefulset-volume-claim-templates)) |
| `envFrom` | Container → `ConfigMap` / `Secret` (envFrom) |
| `env` | Container → `ConfigMap` / `Secret` (valueFrom) |
| `initContainer envFrom` / `initContainer env` | Same for init containers |
| `ingress` | `Ingress` → `Service` (rules / default backend) |
| `service` | Service selector match (`ref('service', …)`) |
| `volumeName` | `PersistentVolumeClaim` → `PersistentVolume` |
| `storageClass` | PVC/PV → `StorageClass` |
| `roleRef` | `RoleBinding` / `ClusterRoleBinding` → `Role` / `ClusterRole` |
| `subject` | Binding → subject (`ServiceAccount`, etc.; often with `reverse`) |
| `provisioner` | `StorageClass` → `CSIDriver` (provisioner name) |
| `namespace` | Namespaced entity → its `v1/Namespace` object |
| `controller` | **Owner reference** with `metadata.ownerReferences[].controller: true` (see [Kubernetes owner references](#kubernetes-owner-references)) |
| `owner` | **Owner reference** without `controller: true` (dependent owner) |
| `objectset-owner` | **Rancher wrangler** `objectset.rio.cattle.io/owner-*` **annotations** on the child object → declared owner resource (`id(...)`); attribute **`origin:objectset`** (see [Rancher objectset owner annotations](#rancher-objectset-owner-annotations)) |
| `related` | `events.k8s.io/v1/Event` → `related` object |
| `regarding` | `events.k8s.io/v1/Event` → `regarding` subject (`RefType` **`regarding`**) |
| `workloadRegardingEvent` | Workload → correlated `Event` (name prefix + `regarding` / legacy `involvedObject`; uses `clusterEntities()`) |
| `podMetrics` | Workload → `metrics.k8s.io/v1beta1/PodMetrics` for its pods (`clusterEntities()`) |
| `source` | `batch/v1/Job` → `Secret` when command contains `redis-initial-password` (Argo CD redis init convention) |

### Labels (synthetic refs from Go, not ref-parsers)

Some `Ref` edges are constructed in command logic rather than from CEL ref-parsers. They reuse the same `Labels` field for display and identification. The constant lives in `core/types/refs.go`.

| Label | Meaning |
| ----- | ------- |
| `namespace` (`RefLabelNamespace`) | Same label name as the built-in Kubernetes parser above. Command logic also uses it for synthetic topological **delete** graphs (`computeNamespaceRefs` in `core/commands/delete.go`) and the matching **orphan delete** ordering in `cli/action/cluster_apply_plan.go` so namespace-scoped resources are removed before the Namespace resource. |

### Tags (hydra-go and embedded parsers)

| Tag | Meaning |
| --- | ------- |
| `optional:ref` | Mirrors Kubernetes `optional: true` on env, envFrom, volume, or projected sources; review skips findings for these edges |
| `optional:startup` | Optional startup / scale ordering (Go may also emit this on synthetic edges) |
| `bootstrap-guard` | Ref-parser **group** tag for cluster apply bootstrap checks (`RefTagBootstrapGuard`) |

### Tags (chart configuration only)

These are **not** defined inside embedded YAML; they appear on **`global.hydra.refs` groups** in Helm values or ConfigMap `data.hydra`:

| Tag | Meaning |
| --- | ------- |
| `uninstall` | Uninstall propagation within the defining app |
| `uninstall-safe` | Cross-app “safe to uninstall” marking |
| `uninstall-force` | Target needs explicit `--force` to delete |
| `backup` | Used with uninstall rules; see uninstall docs for conflicts with other tags |
| `runtime` | Use **with** `uninstall` / `uninstall-force`: ref-ownership predicates apply **only** when the resource id is **not** in any standalone template render (cluster-only). **`hydra gitops uninstall`** and the live **`hydra gitops review`** pass use non-runtime predicates for template-mapped ids and include **`runtime`** groups for cluster-only ids; **`hydra local review`** and the template-id pass of **`hydra gitops review`** omit these groups (`RefTagRuntime`) |

### Attribute types

| Attribute type | Allowed values | Meaning |
| -------------- | -------------- | ------- |
| `origin:generated` | `job`, `controller` | Target is materialized after apply by a job-style workload or a controller/operator |
| `origin:app` | `<appId>` | Owning app; Hydra may inject this for app-defined parsers |
| `origin:workload` | `<workloadId>` | Workload identity for grouping/diagnostics |
| `origin:owner` | `controller`, `dependent` | Kubernetes **owner reference** role: `controller` = `metadata.ownerReferences[].controller: true`; `dependent` = other owner entries |
| `origin:objectset` | `rio.cattle.io` | Rancher wrangler **`objectset.rio.cattle.io/owner-*`** annotation edges ([Rancher objectset owner annotations](#rancher-objectset-owner-annotations)) |
| `origin:source` | `template`, `cluster`, `test` | Which entity set produced the edge: Helm templates, live API inventory (`hydra gitops inspect` / review targets), or **test** harnesses (hydra-go ref tests, `hydra local test refs`). Merged edges may carry **multiple** `origin:source` entries with different values. |
| `key` | `<keyName>` | Explicit `Secret` / `ConfigMap` key selection |
| `regarding` | `<canonical Id>` | Duplicate of Event target id on **`regarding`** edges (alongside endpoint) |
| `related` | `<canonical Id>` | Same pattern for Event **`related`** edges |
| `kubernetes:ownerController` | `true` / `false` | Mirrors `metadata.ownerReferences[].controller` on owner-ref edges |
| `kubernetes:blockOwnerDeletion` | `true` | Present when `blockOwnerDeletion: true` on that owner entry |
| `hydra:parent-via` | `<label>` | Declares a ref-based parent source for workload closure (e.g. `podMetrics`, `objectset-owner`) |
| `hydra:parent-direction` | `incoming` / `outgoing` | Which direction of the ref edge closure should follow |

### Endpoint types (CEL `ref(type, value)`)

| Type | Value | Meaning |
| ---- | ----- | ------- |
| `id` | Entity ID string | `id(gvk, namespace, name)` — standard resource target |
| `provider` | Provider name | Operator dependency (`ref("provider", "<name>")`); **names** are chart-defined |
| `crd` | GVK string | CRD linkage for custom resources |
| `service` | Namespace/name id | Service label selector match |

### Generic patterns from chart configuration

These are **conventions** for Helm / ConfigMap ref parsers, not additional built-in strings:

- **`ref("provider", "<name>")`** — Operator or infrastructure dependencies; `<name>` is chart-specific.
- **`global.hydra.refs` groups** — Optional `tag`, `desc`, `label`, `reverse`, `attributes`, `enabled`; merge rules in [Tags, Desc, and Label](#tags-desc-and-label).
- **`"origin:generated": job` / `"origin:generated": controller`** — Declares materialization semantics for virtual targets (see [Generated targets](#generated-targets-recursive-evaluation-and-fixpoint)).
- **`SopsSecret` → `v1/Secret`** — Same-namespace pattern using `origin:generated: controller` in app parsers (not hard-coded per chart in Go).

### Kubernetes owner references

Embedded rule in `ref-parsers/kubernetes/_all.yaml`: any object with non-empty `metadata.ownerReferences` emits **outgoing** `RefDefinition`s to each owner, with target ID `owner.apiVersion + '/' + owner.kind` (Hydra GVK form), namespace `ns`, and `owner.name`. Two `pick` rows split **`controller: true`** vs other owners: labels **`controller`** and **`owner`**, attributes **`origin:owner: controller`** and **`origin:owner: dependent`**, plus **`kubernetes:ownerController`** / **`kubernetes:blockOwnerDeletion`** when set on the owner entry, and **`reverse: true`** on the parser so consumers treat **visual** and **logical** dependency direction per the [reverse flag](#semantic-roles-of-labels-tags-attributes-and-reverse). Typical sources are **cluster** entities (controllers set owner references); plain templates often omit them.

After refs are built against a **live cluster inventory**, `CanonicalizeOwnerRefTargetsToClusterIDs` rewrites **`Ref.To`** on **`origin:owner`** edges when the owner id’s API **version** differs from the cluster object’s preferred version (same group/kind/namespace/name). Without this, UIs and closure logic that require both endpoints to exist can drop version-skewed owner edges.

### Rancher objectset owner annotations

Embedded rule in `ref-parsers/kubernetes/_all.yaml`: when **`metadata.annotations`** contains **`objectset.rio.cattle.io/owner-gvk`** and **`objectset.rio.cattle.io/owner-name`**, and the GVK string parses successfully, Hydra emits one **outgoing** `RefDefinition` from the annotated object (**`Owner`**) to **`id(<hydraGvk>, ownerNamespace, ownerName>)`**, with **`reverse: true`** on the parser (same contract as [Kubernetes owner references](#kubernetes-owner-references)): the **stored** `Ref` edge remains **child → owner**, but **logical** adjacency (transitive reachability, scale dependency interpretation, TUI incoming/outgoing) treats the edge as **owner → child** so dependency chains read naturally **Deployment → Service →** (for example) **svclb `DaemonSet`**. The **`objectset.rio.cattle.io/owner-namespace`** annotation is optional; when absent, the owner namespace segment is empty (cluster-scoped owners). The **`owner-gvk`** value uses wrangler’s comma form (for example **`/v1, Kind=Service`** or **`apps/v1, Kind=Deployment`**); the CEL helper **`objectsetRioOwnerGvkToHydraGvk`** converts it to Hydra’s **`gvk`** string before calling **`id()`**. Label **`objectset-owner`**; attributes **`origin:objectset: rio.cattle.io`**, **`hydra:parent-via: objectset-owner`**, **`hydra:parent-direction: outgoing`**. Golden coverage: `core/references/testdata/kubernetes/v1/ConfigMap/objectset-rio-owner.*`.

### Events, PodMetrics, and workload-correlated Events

- **`ref-parsers/kubernetes/events.k8s.io_v1/Event.yaml`** — `related` and **`regarding`** edges to resolved subject ids; **`regarding`** picks call **`.refType('regarding')`** and set attribute **`regarding: <idString(...)`** (and **`related`** similarly). These edges do not drive apply ordering.
- **`ref-parsers/metrics.k8s.io_v1beta1/PodMetrics.yaml`** — Workloads emit **outgoing** **`podMetrics`** refs to live **`PodMetrics`** objects whose names match the workload’s pod naming pattern via **`clusterEntities({"namespace": ns, "gvk": "metrics.k8s.io/v1beta1/PodMetrics"})`**, with **`hydra:parent-via: podMetrics`** / **`hydra:parent-direction: incoming`** on the pick row.
- **`ref-parsers/kubernetes/workload_regarding_events.yaml`** — Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob, and Pod workloads link to **`events.k8s.io/v1/Event`** (and legacy **`v1/Event`**) when the event name prefix and **`regarding`** / **`involvedObject`** match the workload; label **`workloadRegardingEvent`**.

Parsers that call **`clusterEntities()`** need a cluster inventory overlay in the CEL environment (review source pass, scale status, tree/inspect merges, etc.); template-only renders without overlay return empty lists for those picks.

**YAML before CEL for constants** — When `label`, `tag`, `attributes`, or `reverse` are **the same for every ref** emitted from a ref-parser row or a `pick` entry, declare them in YAML. Reserve CEL `.label()`, `.tag()`, `.attribute()`, and `.reverse()` for values that **differ per emitted ref** (conditionals, maps, per-item labels). The well-known exception is **`optional:ref`**: it must be attached only on the branch that mirrors Kubernetes `optional: true`, so it usually stays in CEL on that branch even when other metadata is YAML (see [Ref-Parser Format](#ref-parser-format)).

### StatefulSet volume claim templates

Built-in parser in `ref-parsers/kubernetes/apps_v1/StatefulSet.yaml`:

1. **Expected PVCs** — For each `volumeClaimTemplate` with a non-empty `metadata.name`, Hydra emits **outgoing** refs to `v1/PersistentVolumeClaim` IDs named `<templateName>-<statefulSetName>-<ordinal>` for every ordinal in `[spec.ordinals.start, spec.ordinals.start + replicas - 1]`. **`spec.replicas`** defaults to **1** when omitted or null; **`spec.ordinals.start`** defaults to **0** when `spec.ordinals` or `start` is omitted or null. When **`spec.replicas` is 0**, this pass emits **no** expected PVC refs. Label **`volumeClaimTemplate`**; attribute **`"origin:generated": controller`** (StatefulSet controller materializes the PVCs). Virtual target materialization in `core/references/refs.go` (`ResolveVirtualRefs`) can still create placeholder PVC entities for review when the target set does not yet list them.

2. **Live PVC confirmation (cluster review only)** — For **`hydra gitops review`**, reference extraction on **template sources** merges a read-only **live cluster inventory** into the CEL `clusterEntities()` snapshot used by ref parsers (without changing other ref APIs). A second `pick` matches **live** `v1/PersistentVolumeClaim` objects in the same namespace only when:
   - the PVC name matches the same `<templateName>-<statefulSetName>-<digits>` pattern (decimal ordinal suffix), and
   - `metadata.ownerReferences` contains an entry with **`controller: true`**, **`kind: StatefulSet`**, **`apiVersion: apps/v1`**, and **`name`** equal to the StatefulSet being parsed.

This avoids false positives from unrelated PVCs that share a name prefix. **`hydra gitops refs` / tree graphs** do **not** use that overlay on the template pass, so `origin:source` on merged edges stays consistent.

Explicit `spec.template.spec.volumes[].persistentVolumeClaim.claimName` refs are **unchanged** and are **not** deduplicated against volume-claim-template edges in this iteration.

**CEL helpers** — `ordinalRange(start, count)` returns `[start, …, start+count-1]` as a list of `int` (empty when `count <= 0`). `vctPvcOrdinalName(pvcName, templateName, statefulSetName)` returns whether `pvcName` matches the expected prefix and trailing decimal ordinal segment.

## Origin and provenance attributes

Use repeated **`attributes`** (not tags) for stable, machine-readable policy:

- **`"origin:generated": job`** — The target resource is expected to appear **after apply** because a **Job** (or equivalent batch/workload) materializes it (for example Job → Secret).
- **`"origin:generated": controller`** — The target is materialized by a **controller** or operator from a CR or other driving resource (for example `SopsSecret` → `Secret`).
- **`"origin:app": <appId>`** — Which **app** owns or defines the relationship; Hydra may add this automatically for app-defined parsers when not already set.
- **`"origin:workload": <workloadId>`** — Which **workload** identity the edge is associated with for grouping, diagnostics, or downstream heuristics.
- **`"origin:owner": controller`** / **`"origin:owner": dependent`** — Kubernetes **owner reference** role for edges from `metadata.ownerReferences` (see [Kubernetes owner references](#kubernetes-owner-references)).
- **`"origin:objectset": rio.cattle.io`** — Edge from Rancher wrangler **`objectset.rio.cattle.io/owner-*`** annotations (see [Rancher objectset owner annotations](#rancher-objectset-owner-annotations)).

If a parser emits **only** job-style edges, declare **`"origin:generated": job`** once at parser or group level in YAML. If it emits **only** controller-style edges, use **`"origin:generated": controller`** the same way. If **both** kinds appear under the same `predicate`, **split** into two `ref-parsers` rows (same `predicate` allowed) so each row carries the correct constant attributes without conditional CEL.

Use the explicit YAML key **`"origin:generated"`** for these attributes. Shorthand aliases such as **`generated`** are not part of the supported ref-parser schema and should be rejected rather than silently reinterpreted.

### Virtual/runtime materialization in prose

When describing materialization in narrative docs, prefer the attribute name **`origin:generated`** (values `job` | `controller`). Avoid vague terms such as “provisioned” unless you clearly tie them to these values.

## Ref-Parser Format

Ref-parsers are YAML files that define how to discover references for specific Kubernetes resource types. They are embedded into the binary via `//go:embed`.

```yaml
ref-parsers:
  - group: optional-group
    version: optional-version
    kind: optional-kind
    apiVersion: optional-group/version # optional YAML sugar; normalized to group+version
    gvk: optional-group/version/kind # optional YAML sugar; normalized to group+version+kind
    namespace: optional-namespace
    gvkn: optional-group/version/kind/namespace # optional YAML sugar; normalized to gvk+namespace
    name: optional-name
    id: optional-group/version/kind/namespace/name # optional YAML sugar; normalized to group+version+kind+namespace+name
    cel: "<optional residual CEL predicate>"
    predicate: "<alias for cel on the parser row; mutually exclusive with cel>"
    attributes:
      - "<attribute-key>": "<attribute-value>" # optional; each item must have exactly one key/value pair
    tag: [optional-tag] # optional: parser-level tags (union with group, pick, and CEL)
    desc: "optional desc" # optional: parser-level description
    label: "optional-label" # optional: default label when CEL leaves label empty
    reverse: false # optional: parser-level reverse flag (OR with pick and CEL)
    pick:
      - cel: "<CEL expression returning a list of RefDefinition>"
        attributes: [] # optional: only for this pick
        tag: [optional-tag]
        label: "optional-label"
        reverse: false
```

The parser row now has two parts:

- **Structured selector preconditions** — exact matches on `group`, `version`, `kind`, `namespace`, and `name`.
- **Optional residual predicate** — `cel` or **`predicate`** (same field; only one may be set) evaluated only after all structured selector fields match.

At least one selector field or a residual predicate must be present. Built-in `_all.yaml` uses **`predicate`** for global rules (for example `predicate: "true"` and `predicate: "ns != ''"`). YAML sugar fields `apiVersion`, `gvk`, `gvkn`, and `id` are accepted for authoring convenience but are normalized immediately to the canonical selector fields above. If multiple fields disagree with each other, Hydra rejects the parser row as invalid.

**Prefer YAML over CEL** for `attributes`, `tag`, `label`, and `reverse` whenever the value is constant for the whole ref-parser row or for a single `pick` entry. Do **not** use CEL `.label('fixed')` / `.label("fixed")` or `.tag('fixed')` / `.tag("fixed")` when that value applies to **every** ref from the `pick`—use the `label:` and `tag:` fields on the `pick` item (or parser/group defaults when they apply to every pick). Use CEL `.attribute()`, `.tag()`, `.label()`, and `.reverse()` only when the value must **vary per emitted ref** (for example different labels on different branches of a conditional or map). **Exception:** refs that mirror Kubernetes `optional: true` fields use **`.tag('optional:ref')` only on the optional branch** of a ternary; a YAML `tag` on the whole `pick` would incorrectly tag the required branch too, so that tag stays in CEL. Declaring provenance such as `"origin:generated"` or `"origin:workload"` in YAML keeps policies visible without scanning CEL strings.

When a CEL predicate contains constant equality checks for selector fields, lift those checks into YAML instead of leaving them in `cel`. Example: write `gvk: apps/v1/Deployment` plus `cel: 'name.startsWith("worker-")'` instead of `cel: 'gvk == "apps/v1/Deployment" && name.startsWith("worker-")'`.

The `tag`, `desc`, and `label` fields on ref-parser rows follow the same merge patterns as group-level defaults (see [Tags, Desc, and Label](#tags-desc-and-label)). Parser-level `attributes` add repeated relation metadata to every ref emitted by that parser unless overridden by pick-level `attributes` (merged as sets). Each YAML item in `attributes` must be a single-entry object. App-defined ref-parsers (`global.hydra.refs`) may use parser-level `attributes`; for app-owned parsers Hydra can additionally add `origin:app=<owning-app-id>` automatically when that attribute is not already declared explicitly.

When refs need to preserve structured relationship metadata, the builder should emit repeated attributes instead of overloading tags. For `hydra local review`, explicit Secret and ConfigMap key selections should be represented as repeated `RefAttribute{Type: "key", Value: "<key-name>"}` entries on the relation while the endpoint still points to the target resource. The same mechanism should carry parser provenance such as `origin:app=<appId>` and `origin:workload=<workloadId>`.

Parser-wide provenance metadata should therefore be declared like this:

```yaml
ref-parsers:
  - gvk: v1/Secret
    attributes:
      - "origin:app": in-cluster.cluster-infra.kyverno
      - "origin:workload": kyverno-background-controller
    pick:
      - cel: '[refBuilder().outgoing(ref("provider", "kyverno"))]'
```

This parser-level form is intentionally compact in YAML, but it still maps to repeated relation attributes on the resulting refs. Provenance should **not** be encoded as tags, because it is structured metadata rather than a boolean edge marker.

For reusable chart values, parsers will usually declare only `"origin:workload"` and rely on the loader to attach `"origin:app"` from the owning app automatically.

### Selector And Residual CEL

Use the structured selector fields for exact identity matches and reserve `cel` for whatever cannot be expressed as an exact match:

```yaml
gvk: apps/v1/Deployment
kind: Pod
group: rbac.authorization.k8s.io
kind: RoleBinding
namespace: kube-system
name: coredns
cel: 'has(entity.spec.selector) && name.startsWith("frontend-")'
```

### Pick entries (`cel`)

Each `pick` item is a YAML object with a required **`cel`** field: a CEL expression that evaluates to a **list** of lists of `RefDefinition` values (same as before). Optional **`attributes`**, **`tag`**, **`label`**, and **`reverse`** apply only to refs produced by that `pick` and are merged with parser- and group-level fields.

When the same label, reverse flag, or tags would repeat across multiple `pick` entries, declare them once on the ref-parser row or on the ref group instead.

```yaml
pick:
  # Deployment → ServiceAccount (labels vary per reference type — keep .label() in CEL here)
  - cel: "refBuilder().outgoing(id('v1/ServiceAccount', ns, entity.spec.template.spec.serviceAccountName)).label('serviceAccount')"

  # Uniform label + reverse — prefer YAML on the pick row
  - cel: "entity.subjects.filter(s, s.kind == 'ServiceAccount').map(s, refBuilder().outgoing(id('v1/ServiceAccount', s.namespace, s.name)))"
    label: subject
    reverse: true
```

## CEL Environment

### Available Variables

| Variable  | Type     | Description                                                  |
| --------- | -------- | ------------------------------------------------------------ |
| `gvk`     | `string` | GVK string of the entity (e.g. `"apps/v1/Deployment"`)       |
| `group`   | `string` | API group (e.g. `"apps"`)                                    |
| `version` | `string` | API version (e.g. `"v1"`)                                    |
| `kind`    | `string` | Resource kind (e.g. `"Deployment"`)                          |
| `ns`      | `string` | Namespace (empty for cluster-scoped)                         |
| `name`    | `string` | Resource name                                                |
| `entity`  | `map`    | Full entity as map (access `spec`, `metadata`, etc.)         |
| `CRDs`    | `list`   | List of all GVK strings defined by CustomResourceDefinitions |

When the caller registers **cluster inventory support** (review, scale, inspect/tree merges, uninstall closure, etc.), additional functions are available — see [cel.md](cel.md):

| Function | Description |
| -------- | ----------- |
| `clusterEntities()` | All live cluster entities in the overlay snapshot |
| `clusterEntities(selector)` | Filtered subset (`namespace`, `gvk`, `id`, …) |
| `templateEntities()` / `entities()` / `managedNamespaces()` | Template vs merged inventory views |
| `involvedObjectEvents(...)` | Legacy/core `v1/Event` lookup helpers |
| `objectsetRioOwnerGvkToHydraGvk(string)` | Wrangler owner-gvk → Hydra `gvk` |
| `idString(gvk, namespace, name)` | Canonical id string without building an endpoint |
| `matchingServices(ns, labels)` | Service selector match helper for workloads |

### Builder Functions

| Function                   | Description                                                                      |
| -------------------------- | -------------------------------------------------------------------------------- |
| `refBuilder()`             | Creates a new RefDefinition builder                                              |
| `.incoming(endpoint)`      | Adds an incoming endpoint (arrow points toward owner)                            |
| `.outgoing(endpoint)`      | Adds an outgoing endpoint (arrow points away from owner)                         |
| `.label(string)`           | Sets a label on the RefDefinition (only allowed on outgoing references)          |
| `.tag(string)`             | Adds a tag to the RefDefinition                                                  |
| `.desc(string)`            | Sets a description on the RefDefinition                                          |
| `.attribute(type, value)`  | Adds a structured repeated relation attribute                                    |
| `.key(name)`               | Convenience helper for `.attribute("key", name)`                                 |
| `.reverse()`               | Marks the last RefDefinition as reversed                                         |
| `.refType(string)`         | Sets `RefDefinition.Type` on the last ref (e.g. **`regarding`** for Events)      |
| `id(gvk, namespace, name)` | Creates an ID endpoint                                                           |
| `ref(type, value)`         | Creates a custom endpoint (e.g. `ref('crd', gvk)`, `ref('provider', 'kyverno')`) |

For explicit Secret and ConfigMap key selections, parsers should prefer repeated `key(...)` attributes over encoding keys into tags:

```yaml
pick:
  - cel: 'refBuilder().outgoing(id("v1/ConfigMap", ns, "api-config")).key("SPRING_PROFILE").key("LOG_LEVEL")'
    label: env
```

Virtual/runtime-materialized meaning should use the **`origin:generated`** attribute (`job` or `controller`) plus structured attributes such as `"origin:app"` / `"origin:workload"`. It should not rely on a dedicated `RefTypeRuntime`; `RefType` is only needed to distinguish parser-discovered `direct` refs from synthetic `indirect` refs such as topological startup edges.

## Reference Resolution Algorithm

```go
func Refs(l log.Logger, entities entity.Entities, key types.EntityKeyUnstructured, extraParsers ...[]types.RefParser) ([]types.Ref, error)
```

The `key` parameter specifies which unstructured key to use for entity inspection (e.g. `KeyTemplateEntity` or `KeyClusterEntity`). Optional `extraParsers` extend the built-in ref-parsers. Missing entity IDs are handled internally by creating refs to non-existing entities; downstream consumers may later classify those IDs as plain missing targets or as materialized virtual targets after recursive **`"origin:generated": job`** / **`"origin:generated": controller`** expansion.

### Step 1: Load and Compile Ref-Parsers

Ref-parsers are collected from:

1. **Embedded YAML** — files under `core/references/ref-parsers/` (`//go:embed`).
2. **Helm values** — enabled groups under `global.hydra.refs` for the selected apps (`HydraAppRefParsers`).
3. **Hydra ConfigMaps** — `v1/ConfigMap` objects with annotation `hydra-gitops.org/hydra-config` set to a truthy value (`strconv.ParseBool`) and a non-empty `data.hydra` string containing YAML with a top-level `refs` map (same group shape as Helm). Parsed via `RefParsersFromHydraConfigMaps` and merged after Helm parsers. When both a rendered template and a live cluster inventory are consulted (for example `hydra gitops review`), the same ConfigMap resource ID may appear twice; an optional `seen` set deduplicates by entity ID so parser rules are not registered twice.

```text
Embedded ref-parser YAML files
  │
  ├── Helm global.hydra.refs (per app)
  │
  ├── ConfigMap data.hydra (annotated ConfigMaps in the active entity set)
  │
  ▼
For each ref-parser definition:
  1. Normalize selector input (`apiVersion` / `gvk` / `gvkn` / `id`) → canonical `group` / `version` / `kind` / `namespace` / `name`
  2. Compile optional residual CEL/`predicate` → Predicate
  3. Compile pick expressions (CEL) → []Expression
```

When callers pass a **preferred API version map** (from cluster discovery / `NormalizeApiVersions`), `Refs` rewrites **`RefDefinition` id endpoints** to the preferred version per group/kind before index building so template and cluster ids still match.

### Step 2: Extract Reference Definitions

```text
For each entity in entities:
  For each ref-parser:
    1. Compare structured selector fields against entity identity
    2. If selector matches, evaluate residual CEL when present
    3. If the residual CEL also matches (or is absent):
       For each pick expression:
         4. Evaluate expression with entity as context
         5. Collect returned RefDefinitions
```

This produces a flat list of `RefDefinition` objects, each with:

- An **owner** entity (the entity being analyzed)
- An **endpoint** (incoming or outgoing target)
- Optional **label**, **tags**, **desc**, and **reverse** flag

### Step 3: Build Index Maps

```text
RefDefinitions
  │
  ├── Incoming map:  targetId → Set<RefEndpoint>
  │   For each incoming RefDefinition:
  │     Register: endpoint.Id → refEndpoint pointing to owner
  │
  └── Outgoing map:  RefEndpoint → []RefDefinition
      For each outgoing RefDefinition:
        Register: refEndpoint → refDefinition (with owner, label, reverse)
```

### Step 4: Connect Endpoints

```text
For each entry in outgoing map:
  1. Find matching entries in incoming map
  2. For type "id": look up targetId in incoming map
  3. For custom types (e.g. "crd"): match against all incoming entries with same type and value
  4. Create Ref objects: From=owner, To=matchedTarget, Labels, Reverse
```

### Step 5: Handle Missing Entities

Outgoing references of type `"id"` that point to entities not present in the input set still produce `Ref` objects. The `Refs` function creates edges to these missing entity IDs, but it does **not** decide yet whether they remain plain `"app:missing"` targets or become virtual/materialized targets after recursive **`"origin:generated": job`** / **`"origin:generated": controller`** evaluation. That consumer-specific classification happens later from the stabilized graph and parser-defined provenance metadata.

### Step 6: Merge and Sort

- Multiple RefDefinitions producing the same `(From, To)` pair are merged:
  - **Labels**: union of all labels
  - **Tags**: union of all tags
  - **Attributes**: union of all `(type, value)` pairs, sorted for deterministic output
  - **Reverse**: OR'd (if any ref is reversed, the merged ref is reversed)
  - **Desc**: first non-empty desc wins; a warning is logged if a different desc is discarded
- Results are sorted by `(From, To)` for deterministic output

## Common reference types (examples)

The canonical list of **built-in relationship labels** is [Reference vocabulary (hydra-go)](#reference-vocabulary-hydra-go). Supplemental **source → target** examples for common Kubernetes fields:

- `serviceAccount` — Workload → ServiceAccount (`serviceAccountName`)
- `volume` — Workload → PVC / ConfigMap / Secret (volume mounts)
- `env` — Workload → ConfigMap / Secret (`env.valueFrom`)
- `envFrom` — Workload → ConfigMap / Secret (`envFrom`)
- `imagePullSecret` — Workload → Secret (`imagePullSecrets`)
- `roleRef` — RoleBinding → Role / ClusterRole (`roleRef`)
- `subject` — RoleBinding → ServiceAccount (`subjects`; often `reverse`)
- `ingress` — Ingress → Service (rules)
- `service` — Service → Workload (label selector match)
- `volumeClaimTemplate storageClass` — StatefulSet → StorageClass (`volumeClaimTemplates[*].spec.storageClassName`)
- `storageClass` — PVC / PV → StorageClass (`spec.storageClassName`)
- `provisioner` — StorageClass → CSIDriver (`provisioner` field)
- `crd` — CustomResource → CRD (CRD-defined GVK)
- `controller` / `owner` — Child → Owner (`metadata.ownerReferences`)
- `objectset-owner` — Object with **`objectset.rio.cattle.io/owner-*`** annotations → declared owner (`id`, [Rancher objectset owner annotations](#rancher-objectset-owner-annotations))
- `regarding` / `related` / `workloadRegardingEvent` — Event correlation ([Events, PodMetrics, and workload-correlated Events](#events-podmetrics-and-workload-correlated-events))
- `podMetrics` — Workload → metrics-server **`PodMetrics`**

## Generated targets, recursive evaluation, and fixpoint

Some targets exist only because another object **materializes** them (for example a custom resource that creates a `Secret`). App-defined ref-parsers declare those edges with attribute **`"origin:generated": job`** (for example a batch `Job` that writes a `Secret`) or **`"origin:generated": controller`** (operators/controllers) and attach structured **`key`** attributes when the manifest exposes explicit key lists.

**Provenance from ref metadata (not hard-coded Go scans)** — Which API group/kind produces which `Secret`/`ConfigMap` name and keys is determined by **embedded parsers plus Helm `global.hydra.refs` and ConfigMap-merged parsers**, not by maintaining a fixed list of CRD shapes inside `hydra-go`. Charts document the operator contract in CEL (`predicate` / `pick`) and attributes; Hydra does not infer virtual targets by scanning all entities for a single hard-coded GVK-specific field.

**Recursive evaluation until a fixpoint** — For any consumer that needs to know whether a referenced `Secret`/`ConfigMap` “exists” or which keys it will have when only upstream producers are present, Hydra **repeatedly applies** the ref graph over the active entity set: each iteration may surface new **virtual** targets implied by **`"origin:generated": job`** / **`"origin:generated": controller`** edges (same namespace as the producer unless the ref declares otherwise). Iteration stops when a round makes **no** new materialized target id (and associated key metadata) visible—i.e. a **fixpoint**. Only the stabilized view is used for missing-target checks, key validation, and downstream dependency features. This is **generic for all ref consumers** (review, apply ordering, UI, diagnostics), not a `review refs`-only code path.

**SOPS modeled generically** — The common case “`SopsSecret` CR → `v1/Secret` in the **same namespace**” is expressed like any other controller-materialized edge: ref-parsers emit an `id(...)` endpoint for the Kubernetes `Secret` with **`"origin:generated": controller`** (and optional labels). There is no separate special case that only exists in Go while charts stay silent.

**Policy mirroring** — Controllers such as Kyverno that **clone or generate** Secrets continue to select **`v1/Secret`** as the object to mirror or mutate. Hydra may show `SopsSecret`→`Secret` in the graph for provenance, but Kyverno policies in typical setups still point at the **Secret**, not the `SopsSecret`.

## Review-Oriented Validation Boundaries

The current reference model is resource-oriented. `RefEndpoint` stores only an endpoint `Type` and `Value`; for `id` endpoints, the value is the referenced entity ID (`group/version/kind/namespace/name`). There is no slot for a Secret or ConfigMap key.

This matters for both review variants and any other ref-based feature that shares the same resolution rules:

- Built-in workload parsers already identify the referenced Secret or ConfigMap resource for locations such as `env.valueFrom`, init-container variants, `envFrom`, and projected volume sources.
- Local `hydra local review` runs ref extraction on **selected-app** template entities but resolves target existence against the **union of templates for all effectively enabled apps** on the same cluster (not only the CLI-selected apps).
- `hydra gitops review` should reuse the same relation model for rendered selected sources, but resolve target existence against **all** live cluster resources.
- Missing-key review should rely on structured repeated relation attributes rather than on tags or endpoint identity. Explicit key selections belong in `attributes[{type: "key", value: "..."}]`.
- Key validation must inspect the concrete source fields that carry explicit Secret/ConfigMap keys, emit those keys as relation attributes, and correlate them with the resolved target resource.
- `envFrom` remains an existence-only check because it does not select a single key and therefore contributes no `key` attribute.

As long as endpoints stay resource-level, review findings should be attached to the source resource and include the target resource separately. Key information belongs in repeated relation attributes, not in the endpoint identity.

## review refs Semantics

### Shared implementation

`hydra local review` and `hydra gitops review` should call the same review orchestration with a swappable target entity source: **local** supplies targets from a **full-cluster template render** (effectively enabled apps only on the affected cluster); **cluster** supplies targets from a **full live cluster inventory**. **Sources** always come from the template entities of the apps selected for the review (after `--exclude-app`). Shared steps after the required renders include collecting app ref parsers, running `references.Refs` on the source entity set, enriching explicit key attributes, target-key normalization against the target entity set, then grouping and sorting findings into a final in-memory list before stdout emission.

### Kubernetes bootstrap catalog (review refs)

Both `hydra local review` and `hydra gitops review` share a **built-in catalog** of Kubernetes default resource IDs (non-RBAC core objects plus RBAC bootstrap `ClusterRole` and `ClusterRoleBinding` names aligned with Kubernetes upstream `plugin/pkg/auth/authorizer/rbac/bootstrappolicy/testdata`). The catalog is **upstream-only** (no distribution-specific entries).

- **Local review** merges synthetic template entities for those IDs into the **template target** set (after clone materialization) so references to bootstrap roles and cluster defaults resolve without `missing target resource`. Filtering uses the effective Kubernetes **minor** version from Hydra values (Helm rendering version when no CLI override applies); if parsing fails, Hydra uses a permissive default so the full catalog is included.
- **Namespace-local default targets (review only)** — After the static bootstrap merge, local review also merges a **kubernetes-defaults** bundle per namespace that appears in the **template target** render: `v1/ServiceAccount/<ns>/default`, `v1/ConfigMap/<ns>/kube-root-ca.crt` (with synthetic `data.ca.crt` so key-aware review can validate the usual key), and the `v1/Namespace` object. Documents already present in templates are deduplicated the same way as `RenderCluster`. This models the API server’s per-namespace defaults without requiring charts to render them.
- **Cluster review** does **not** inject synthetic targets into the live inventory. Instead, **`hydra gitops review cluster`** (not **`hydra gitops review app`**) runs an audit after reference validation: any expected catalog ID missing from the cluster is reported as **`missing cluster default resource`**. The server **minor** from discovery filters RBAC entries that only exist on newer Kubernetes versions. If discovery cannot report a version, the audit is skipped. For a missing bootstrap ID, the ref pass **suppresses** `missing target resource` so operators see a single bootstrap finding per ID.
- **Cluster review — auxiliary namespace defaults** — Separately from the live inventory, Hydra builds the same namespace-default bundle from the **full enabled-app template render** (`renderedAllApps`). For references that qualify as **namespace-local defaults** only—target is `ServiceAccount/default` or `ConfigMap/kube-root-ca.crt` in the **same** namespace as the namespaced source—target resolution treats the live object **or** the auxiliary template-derived object as sufficient. Cluster-scoped sources and explicit cross-namespace references (for example a `RoleBinding` subject `ServiceAccount` in another namespace) **do not** use this shortcut; they still require a live target. This logic does **not** depend on API server version discovery; it still runs when the bootstrap audit is skipped.

### hydra local review

`hydra local review` renders the selected apps for **sources** and separately materializes **target** templates for all effectively enabled apps on the same cluster (derived from the selected `appId` set). It does not use Kubernetes clients.

- Source scope is defined by `appId...` plus `--exclude-app` (only these apps supply entities that may appear as finding sources).
- Target resolution uses the **full enabled-app template set** for that cluster; `--exclude-app` must not remove another app's templates from the target index when that app stays enabled.
- Apps with `enabled: false` are not rendered and therefore cannot be sources or targets in the local target set.
- Missing target objects use the finding text `missing target resource` when neither a matching `Secret`/`ConfigMap` (or other referenced object) exists in the target set nor any ref in the stabilized ref set (after **recursive** `"origin:generated": job` / `"origin:generated": controller` expansion to a **fixpoint**) accounts for that target id. Ref-parsers in Helm `global.hydra.refs`, ConfigMap-merged parsers, and embedded parsers declare materialized targets with these **attributes**; **ref labels** are not used for this check. Target key sets for edges with **`"origin:generated": controller`** come from **relation attributes and parser-declared structure** (for example repeated `key(...)` from `pick` expressions), not from ad hoc Go code that knows one CRD’s field names while charts omit ref metadata.
- Missing `Secret` / `ConfigMap` keys are reported only for explicit key selectors that emitted `attributes[type=key]`.
- Multiple source sites are grouped when both the target and the message are identical.
- A non-empty finding set is a command failure and should therefore result in a non-zero exit code.
- **Stdout contract** - Grouping and sorting may require the full finding set first; "streaming" here means the **write** phase only: after the final ordered list exists, default text output is written via `WriteReviewFindingsGroupedText` (by message type, omitting empty `sources`). **`--yaml`:** each finding as the next YAML sequence element, without marshaling the entire slice as one YAML value at the end (so large outputs still avoid a single giant marshal and can flush incrementally).
- YAML coloring uses the standard CLI `ColorFlag` wiring: default follows TTY auto-detect, `--color` forces color, `--no-color` disables it, and `color-mode` remains mutually exclusive with the boolean flags just like other YAML-emitting commands.
- Debug-level logs should bracket the slow post-render phases—`HydraAppRefParsers`, `references.Refs`, explicit key enrichment, target-key normalization, grouping and sorting—so operators can see which stage consumes time without mixing those diagnostics into the YAML stream (keep structured findings on stdout, logs on the logger).

### hydra local refs

`hydra local refs <cluster> <id>` renders **all** effectively enabled apps on the named cluster (same template entity set as using `references.Refs` over a full-cluster selected-app render), applies the same explicit key attribute enrichment as review, then prints every `types.Ref` edge whose `from` or `to` equals `id` as a YAML sequence. It does not run target validation or emit review findings; stdout is the filtered ref list only. Implementation: `commands.ClusterRefsTouchingId`.

### hydra gitops review

`hydra gitops review` renders the selected apps locally for source-side review, but resolves targets against **all** live cluster resources in the selected cluster.

- Source scope is defined by `appId...` plus `--exclude-app` (only these apps supply finding sources).
- Target resolution uses the **complete** live inventory; `--exclude-app` must not filter that list. Selected rendered sources may legitimately resolve to targets that were never part of the local render pass.
- Apps with `enabled: false` are not rendered by Hydra and supply **no sources**; **target** resolution still uses the **full** live inventory (API snapshot), not a template-derived subset.
- Missing target objects use the finding text `missing target resource` when neither a matching live object nor any ref after the same **fixpoint** expansion of **`"origin:generated": job`** / **`"origin:generated": controller`** edges accounts for the reference (same rule as local review).
- Missing `Secret` / `ConfigMap` keys are reported only for explicit key selectors that emitted `attributes[type=key]`.
- Multiple source sites are grouped when both the target and the message are identical.
- A non-empty finding set is a command failure and should therefore result in a non-zero exit code.
- The same stdout contract (group/sort then sequential emission: default text, optional `--yaml` for YAML elements), `ColorFlag` behavior, and debug instrumentation boundaries match `hydra local review`; only the target source differs.

### Relation Attributes for Key-Aware Review

Both review commands should build on top of the ordinary resource-level relation and enrich it with repeated attributes for explicit key selections:

```yaml
from: apps/v1/Deployment/demo/api
to: v1/ConfigMap/demo/api-config
labels: [env]
attributes:
  - key: SPRING_PROFILE
  - key: LOG_LEVEL
```

This keeps the target resource address stable while preserving all explicitly selected keys on the edge itself. Multiple key references on the same edge are expected and should survive merge as a deduplicated, sorted attribute set.

## Reverse References

The `reverse` flag swaps the logical dependency direction and visual arrow direction without changing the data model fields (From/To remain unchanged):

```text
Data model:   RoleBinding ──subject──→ ServiceAccount    (from=RB, to=SA)
Visual:       RoleBinding ←───────── ServiceAccount      (arrow points from SA to RB)
```

This is used to create intuitive visual chains:

```text
Deployment → SA → RB → Role
                  ↑
                  reverse=true on RB→SA reference
```

Without `reverse`, the graph would show `RB → SA` and `RB → Role` as two separate arrows from RB, losing the chain visualization.

## Service Matching

Services use label selectors to find matching workloads. This requires a special matching mechanism:

1. Build a service map: namespace → `ServiceInfo` (name, selector labels)
2. For each workload entity: check if any service's selector matches the workload's labels
3. If match found: create a `service` reference from Service to Workload

## Test Structure

Reference tests use the golden file pattern. There are two **mechanical** categories (built-in golden files vs chart-level `hydra local test refs`), described below. **Planned or ongoing coverage** for ref metadata, virtual targets, and parser conventions is consolidated in the next subsection—this is the single place to read for “what to test” beyond the file-layout descriptions.

### Tests to add or extend (metadata, provenance, and consumers)

Use this subsection when changing ref-parser YAML, `origin:*` attributes, labels, `optional:ref`, or recursive virtual-target behavior. It merges fixpoint/review expectations with the checklist for declarative provenance and naming conventions.

#### Review and recursive virtual targets

- **`core/commands/review_refs_test.go`** (and CLI wiring tests if behavior crosses the boundary) — Cases where a referenced `Secret`/`ConfigMap` appears only after **multiple** rounds of virtual expansion; assert **no** `missing target resource` when the chain is fully declared in ref metadata, and assert **failure** when an intermediate **`origin:generated`** edge is absent. Mirror the same cases for **`hydra gitops review`** when the live inventory differs only in ordering or redundant duplicates.
- **Kubernetes bootstrap catalog** — Assert **local** review does not report `missing target resource` for a `ClusterRoleBinding` that references bootstrap `ClusterRole` `view` when that role is absent from rendered templates. Assert **`hydra gitops review cluster`** emits **`missing cluster default resource`** when the live inventory omits a bootstrap ID and does **not** duplicate `missing target resource` for the same ID when a source still references it.
- **`core/references/` golden tests** — New or updated `.given.yaml` / `.parsers.yaml` / `.expected.yaml` under `core/references/testdata/` that encode multi-hop **`origin:generated`** edges **without** relying on Go special cases; use `-update` only when outputs are intentionally changed.

#### YAML-declared materialization

Assert that refs that should declare runtime materialization (for example Job→Secret-style edges) carry **`"origin:generated": job`** or **`"origin:generated": controller`** in merged attributes when that provenance is declared from Helm **`global.hydra.refs`** YAML, so review and recursive virtual-target resolution stay consistent.

#### Built-in parser metadata

Golden or unit tests for parsers that emit edges where **labels** must match the **relation/target role** (for example subject-binding edges), so labels stay accurate relative to endpoint kinds and naming conventions in [Semantic roles](#semantic-roles-of-labels-tags-attributes-and-reverse).

#### Explicit `origin:generated` key

Keep or extend coverage in **`RefAttributesFromParserAttributes`** (or equivalent) so parser attributes accept the explicit key **`"origin:generated"`** and reject unsupported shorthand aliases such as **`generated`** with a clear validation error.

#### optional:ref

Where parsers attach **`optional:ref` only on the branch** that mirrors Kubernetes **`optional: true`**, tests should confirm required branches stay untagged and that review logic skips findings for those edges as documented.

#### Shared ref consumers

Where other packages consume the post-`Refs()` graph for ordering or UI-facing summaries, add narrow tests that the **fixpoint-expanded** ref list is what those callers see (not only review).

#### Cleanup

Remove or rewrite tests that depended on **hard-coded** knowledge of a single CRD’s fields (for example `SopsSecret`-specific scans in application code); prefer fixtures that supply the same contract via **`global.hydra.refs`-shaped** extra parsers.

### Built-in Parser Tests (in hydra-go)

Golden tests live under `core/references/testdata/`. `TestFindRefsParameterized` in `refs_test.go` walks the tree, discovers every `*.given.yaml`, and runs one subtest per case (currently **~150** cases). Test names mirror the path after `testdata/` (for example `kubernetes/v1/Pod/envfrom-configmap`).

#### Path layout

```text
testdata/<domain>/<groupVersionDir>/<Kind>/<caseBasename>.{given,expected,parsers}.yaml
```

- **`<groupVersionDir>`** — API group with dots replaced by underscores, plus `/version` (for example `apps_v1`, `rbac.authorization.k8s.io_v1`, `events.k8s.io_v1`).
- **`<caseBasename>`** — Describes the scenario (for example `envfrom-configmap`, `volumeclaimtemplate-pvc-ordinals`, `objectset-rio-owner`).
- **`kubernetes/`** — Uses **embedded** ref-parsers only (no `.parsers.yaml` in normal workload matrix cases).
- **Other top-level domains** — Operator or cross-app scenarios; almost always ship a sibling **`.parsers.yaml`** with the chart-shaped rules under test.

#### Top-level tree (current)

```text
core/references/testdata/
├── kubernetes/                         # built-in parsers (~134 cases)
│   ├── apps_v1/                        # ControllerRevision, DaemonSet, Deployment, ReplicaSet, StatefulSet
│   ├── batch_v1/                       # CronJob, Job
│   ├── v1/                             # ConfigMap, Pod, PV, PVC, ReplicationController, Secret, ServiceAccount
│   ├── rbac.authorization.k8s.io_v1/  # ClusterRole*, Role*, RoleBinding*
│   ├── networking.k8s.io_v1/           # Ingress (backend-service, default-backend)
│   ├── storage.k8s.io_v1/              # StorageClass/provisioner
│   ├── events.k8s.io_v1/               # Event/regarding (+ related in same fixture)
│   ├── discovery.k8s.io_v1/           # EndpointSlice/service
│   ├── authentication.k8s.io_v1/       # TokenRequest, TokenReview (standalone)
│   └── authorization.k8s.io_v1/        # *SubjectAccessReview (standalone / serviceaccount)
├── argocd/                             # argocd-server runtime refs, repository Secret
├── cert-manager.io/                    # Certificate → cert-manager provider
├── clickhouse.altinity.com/           # ClickHouseInstallation
├── clickhouse-keeper.altinity.com/     # ClickHouseKeeperInstallation
├── cross-app/                          # kyverno-image-pull-secret, storage-csi-chain, tags-and-desc
├── isindir.github.com/                 # SopsSecret (with-operator, argocd-client-secret)
├── kafka.strimzi.io/                   # Kafka, KafkaTopic, KafkaUser
├── kyverno.io/                         # ClusterPolicy sample
└── monitoring.coreos.com/              # ServiceMonitor → prometheus provider
```

Operator-specific rules are **not** embedded in the binary; golden files under `isindir.github.com/`, `kafka.strimzi.io/`, `argocd/`, etc. load them from **`.parsers.yaml`** via `ParseRefParsers()` as `extraParsers`.

#### Files per case

| File | Role |
| ---- | ---- |
| **`.given.yaml`** | Input entities (multi-document YAML), parsed with `KeyClusterEntity` |
| **`.expected.yaml`** | Golden `refDefinitions` and `refs` (`ElementsMatch` in tests) |
| **`.parsers.yaml`** | Optional extra ref-parser YAML (operator / cross-app cases) |

The harness runs `RefDefinitions` and `Refs` on the given set, then stamps **`origin:source: test`** on every ref (`AnnotateRefsWithSource`) so golden `refs` include provenance like production merges.

#### Workload matrix (kubernetes)

Most controller kinds under `apps_v1/`, `batch_v1/`, and `v1/ReplicationController` share the same **case basenames** (each is a separate golden directory):

`configmap-volume`, `secret-volume`, `pvc-volume`, `projected-volume`, `imagepullsecret`, `serviceaccount`, `envfrom-configmap`, `envfrom-secret`, `env-configmapkeyref`, `env-secretkeyref`, `initcontainer-envfrom-configmap`, `initcontainer-env-secretkeyref`.

**Pod-only** extras: `env-secretkeyref-optional`, `service-selector`.

**StatefulSet-only** extras: `volumeclaimtemplate-storageclass`, `volumeclaimtemplate-storageclass-null-only`, `volumeclaimtemplate-storageclass-null-mixed`, `volumeclaimtemplate-pvc-ordinals`.

**Storage chain** (built-in, no `.parsers.yaml`): `v1/PersistentVolumeClaim/{storageclass,persistentvolume,storageclass-null}`, `v1/PersistentVolume/storageclass`, `storage.k8s.io_v1/StorageClass/provisioner`. End-to-end **CSI provider** wiring is in **`cross-app/storage-csi-chain`** (`.parsers.yaml` + `.given.yaml`).

**Kubernetes metadata / events**: `v1/ConfigMap/objectset-rio-owner`, `events.k8s.io_v1/Event/regarding`.

#### Parsers that need cluster inventory in goldens

Built-in picks that call **`clusterEntities()`** (PodMetrics on controllers, workload→Event correlation, StatefulSet live-PVC confirmation in review) only see objects present in the **same** `.given.yaml` (or in dedicated unit tests), because `TestFindRefsParameterized` passes an **empty** cluster overlay. Examples:

- **`v1/Pod`** → **`PodMetrics`** with the **same** `metadata.name` uses a direct `id(...)` edge (no `clusterEntities`); see `regarding.given.yaml` / `regarding.expected.yaml`.
- **`TestRefsTemplateDeploymentToClusterPodMetricsWithOverlay`** and **`statefulset_vct_cluster_overlay_test.go`** cover overlay-specific behavior outside the golden walk.
- **`ref_parsers_workload_events_audit_test.go`** / **`ref_parsers_podmetrics_audit_test.go`** assert embedded YAML contains `workloadRegardingEvent` / `clusterEntities` picks.

Add new goldens for `workloadRegardingEvent` or overlay-only StatefulSet PVC confirmation by including the correlated **Event** / **PVC** documents in `.given.yaml`, or by extending the dedicated overlay tests above.

#### Regenerating goldens

From `hydra/hydra-go`:

```bash
go test -count=1 ./core/references/ -update
```

Or update references **and** other module goldens (including `core/view`):

```bash
./update_testdata.sh
```

See also `core/references/AGENTS.md`.

### App-Defined Parser Tests (in Charts via `hydra local test refs`)

Each operator chart that defines `global.hydra.refs` should also include golden file tests in a `test/refs/` directory:

```tree
charts-repository/apps/<scope>/<chart>/
    values.yaml          # contains global.hydra.refs
    test/refs/
        <testname>.given.yaml
        <testname>.expected.yaml
```

These tests are executed via the `hydra local test refs` CLI command (see [details/commands.md](commands.md)). The command loads the chart's `global.hydra.refs` as `extraParsers` alongside the built-in parsers, then runs the same golden file comparison logic.

Charts that map CSI drivers to concrete workloads should add chart-level ref tests that verify both sides of the provider match, for example `CSIDriver -> provider("nfs.csi.k8s.io/node")` and `DaemonSet/csi-nfs-node <- provider("nfs.csi.k8s.io/node")`.

Use `--update` to regenerate `.expected.yaml` files:

```bash
hydra local test refs <appId...> --hydra-context ./gitops --update
```

## Tags, Desc, and Label

Tags, desc, and label annotate refs with semantic metadata. They can be set in two ways:

### YAML-Level (group defaults)

When defining ref-parser groups (in app `values.yaml` under `global.hydra.refs`), `tag`, `desc`, `label`, and `priority` can be set at the group level. These values apply to ALL refs produced by the group's ref-parsers:

```yaml
global:
  hydra:
    refs:
      image-pull-secrets:
        tag: [kyverno]
        priority: -2
        desc: "Image pull secrets cloned from sops-secrets-operator"
        label: clone-source
        ref-parsers:
          - gvk: v1/Secret
            name: image-pull-secret
            cel: 'ns != "sops-secrets-operator"'
            pick:
              - cel: '[refBuilder().outgoing(id("v1/Secret", "sops-secrets-operator", "image-pull-secret"))]'
```

### When to use CEL builders for `tag` / `label` / `desc`

Prefer YAML `tag`, `label`, and parser/group `desc` as shown above. Use builder `.tag()`, `.label()`, or `.desc()` in CEL only when the value is **not** the same for every ref from that `pick` (see exception for `optional:ref` under [Ref-Parser Format](#ref-parser-format)).

```yaml
pick:
  - cel: 'refBuilder().outgoing(id("v1/Secret", ns, "x")).desc("Clone of original")'
    tag: [kyverno]
    label: clone-source
```

### Merge Rules

- **`desc`**: Parser- and group-level `desc` fill in when the CEL builder left `desc` empty.
- **`label`**: If a **`pick`** item sets `label` in YAML, that value wins for refs from that pick. Otherwise the CEL builder’s `.label()` is kept if set; otherwise parser- and group-level `label` apply when still empty.
- **`tag`**: **Union** of group tags, parser-level `tag`, pick-level `tag`, and any `.tag()` values from CEL.
- **`priority`**: For app-defined `global.hydra.refs` groups, group-level `priority` is the default ownership priority for all contained parsers. A parser-level `priority` overrides the group value for that parser.
- **`reverse`**: **True** if any of the following is true: CEL `.reverse()`, parser-level `reverse`, or pick-level `reverse`.
- **`attributes`**: **Merged** (set union) in order: CEL-emitted attributes, then parser/group attributes, then pick-level attributes.

### Tag Semantics

Tags serve two purposes:

**Uninstall behavior**: Special tags control what happens during `hydra gitops uninstall`:

- **`uninstall`** — If the source entity is deleted, the target entity may also be deleted. This rule only applies within the app where it was defined.
- **`uninstall-safe`** — Works across apps. App A defines an uninstall-safe rule. When the user calls install of App B, which has a source entity of this rule, untracked resources can be marked as "safe for uninstallation". Examples: untracked reports or events created by App A that reference a resource in App B.
- **`uninstall-force`** — The target resource belongs to the source resource, but it is important and deletion must be explicitly allowed with `--force`. Examples: PVCs or cluster-created secrets that should be backed up, such as Let's Encrypt certificates managed by cert-manager.

**Startup behavior**: The `optional:startup` tag marks refs that should influence the optional part of startup ordering, but must not block required startup:

- **`optional:startup`** — The dependency is informational for startup ordering and is only considered after all required workloads have been started. Missing optional providers are ignored. Typical examples are monitoring CRs such as `ServiceMonitor` / `PodMonitor` that should not force an operator or application into the required startup chain.

**Provider identification via provider endpoints**: See [Provider-Based Operator Dependencies](#provider-based-operator-dependencies) below.

**Reference validation** — Refs tagged **`optional:ref`** mirror Kubernetes **`optional: true`** on env, volume, or projected sources. Review and similar checks **skip** findings for those edges so absent optional targets do not fail validation; see the overview in [references.md](../references.md).

## App-Defined Ref-Parsers

In addition to the embedded ref-parsers, apps can define their own ref-parsers in `global.hydra.refs`. These parsers run against all template entities (in `hydra-ui` and `apply`) or all cluster entities (in `uninstall`), depending on context.

### YAML Format

```yaml
global:
  hydra:
    refs:
      group-name:
        tag: [tag1, tag2]
        desc: "Optional description"
        label: "optional-label"
        enabled: true # optional, default true; set false in parent charts to disable
        ref-parsers:
          - cel: "<residual CEL expression>"
            pick:
              - cel: "<CEL expression returning RefDefinition list>"
```

### Provider Endpoint Type

For uninstall ref-parsers where matched entities need to point back to a logical app (rather than a specific entity), use `ref("provider", "<tag>")`. A dedicated Hydra ref `label` is optional here and should only be added when the graph needs a stable relationship name beyond the provider endpoint itself:

```yaml
ref-parsers:
  - cel: 'clusterEntity.labels().getOrEmpty("app.kubernetes.io/managed-by") == "kyverno"'
    pick:
      - cel: '[refBuilder().outgoing(ref("provider", "kyverno"))]'
```

### enabled Field

Groups can be disabled by setting `enabled: false`. This is useful for parent chart overrides where a child chart defines ref groups that should not apply:

```yaml
global:
  hydra:
    refs:
      some-group:
        enabled: false
```

When `enabled` is not specified, it defaults to `true`.

## Provider-Based Operator Dependencies

Custom Resources (CRs) and selected infrastructure resources require their operator or node plugin to be running before they can be applied. This dependency is modeled using `ref("provider", "<name>")` endpoints in built-in ref-parsers and chart-defined parsers:

- **CRs** emit `outgoing(ref("provider", "<name>"))` — they depend on a provider
- **Operator Deployments** emit `incoming(ref("provider", "<name>"))` — they are the provider

The standard endpoint matching in `Refs()` connects outgoing to incoming `ref("provider", ...)` endpoints, creating a Ref from the CR to the operator Deployment. This works identically to the CRD matching pattern (`ref("crd", gvk)`).

Unmatched provider endpoints (no incoming counterpart) are silently discarded — only `id` endpoints generate "missing entity" refs. This is especially important for optional monitoring dependencies: a chart may emit `ref("provider", "prometheus")` with tag `optional:startup`, and startup still proceeds when no Prometheus operator workload is present.

### App-Defined Operator Parsers

Operator-specific ref-parsers are **not** embedded in the hydra-go binary. They live in the Helm chart of the respective operator as `global.hydra.refs` values (or in annotated Hydra ConfigMaps). This keeps operator knowledge close to the operator chart and allows independent evolution.

Representative **hydra-go** goldens live under `core/references/testdata/<domain>/` (see [Built-in Parser Tests](#built-in-parser-tests-in-hydra-go)); chart-level cases use `charts-repository/.../test/refs/`.

| Operator | Provider tag | Chart (typical) | Outgoing (CR / workload) | Incoming (operator workload) | Golden / chart tests |
| -------- | ------------ | --------------- | ------------------------ | ---------------------------- | -------------------- |
| sops-secrets-operator | `sops` | `sops-secrets-operator` | `isindir.github.com/v1alpha3/SopsSecret` → provider; same-namespace `Secret` (`origin:generated: controller`, reverse) | `sops-secrets-operator` Deployment | `testdata/isindir.github.com/.../with-operator`, `argocd-client-secret`; cross-app `kyverno-image-pull-secret` |
| kyverno | `kyverno` | `kyverno` | all `kyverno.io/*` → provider | `kyverno-admission-controller`, `kyverno-background-controller` | `testdata/kyverno.io/v1/ClusterPolicy/disallow-latest` |
| cert-manager | `cert-manager` | `cert-manager` | `cert-manager.io/*` → provider | `cert-manager`, `cert-manager-webhook` | `testdata/cert-manager.io/v1/Certificate/my-cert` |
| prometheus-operator | `prometheus` | `kube-prometheus-stack` | `monitoring.coreos.com/*` → provider | `kube-prometheus-stack-operator` | `testdata/monitoring.coreos.com/v1/ServiceMonitor/my-monitor` |
| strimzi | `strimzi` | `operator-kafka` | `kafka.strimzi.io/*` → provider; KafkaUser/KafkaTopic → Kafka CR (`strimzi.io/cluster`); KafkaUser → `Secret` (reverse) | `strimzi-cluster-operator` | `testdata/kafka.strimzi.io/v1beta2/{Kafka,KafkaUser,KafkaTopic}/...` |
| clickhouse-operator | `clickhouse-operator` | `operator-clickhouse` | `clickhouse.altinity.com/*`, `clickhouse-keeper.altinity.com/*` → provider | `operator-clickhouse` | `testdata/clickhouse.altinity.com/.../my-chi`, `clickhouse-keeper.altinity.com/.../my-keeper` |
| argocd | — (no provider) | `argocd` | **`argocd-server`** Deployment → `ConfigMap` / `Secret` with label **`runtime`** (id endpoints, cluster/template semantics via chart **`tag: [runtime]`** groups); **repository** `Secret` → **incoming** `ref("argocd.argoproj.io/repository", <git-url>)` | — | `testdata/argocd/apps_v1/Deployment/argocd-server`, `argocd/v1/Secret/repository` |

**Argo CD** does not use the provider pattern for the server workload: runtime association is modeled as **direct `id` refs** with display label **`runtime`**, not `ref("provider", …)`. Repository credentials use a **custom endpoint type** (`argocd.argoproj.io/repository`) matched on the **incoming** side of the labeled repository `Secret`.

### Example: SopsSecret Parser (chart values, generic same-namespace Secret)

The following illustrates the **generic** pattern “custom resource → `v1/Secret` in the **same namespace**” using CEL over the CR’s spec and **YAML `attributes`** for **`"origin:generated": controller`** (preferred over `refBuilder()...attribute("origin:generated", ...)` in `pick` expressions). Concrete field names (`secretTemplates`, template entries) belong to the **operator chart’s** contract and are **not** hard-coded in Hydra. The provider ref does **not** carry **`origin:generated`**—use two `ref-parsers` entries with the same selector (or same residual `cel`) when one parser emits mixed edges.

```yaml
global:
  hydra:
    refs:
      sops-operator:
        ref-parsers:
          - gvk: isindir.github.com/v1alpha3/SopsSecret
            attributes:
              - "origin:generated": controller
            pick:
              - cel: 'entity.spec.secretTemplates.map(t, refBuilder().outgoing(id("v1/Secret", ns, t.name)))'
                label: sops
                reverse: true
          - gvk: isindir.github.com/v1alpha3/SopsSecret
            pick:
              - cel: '[refBuilder().outgoing(ref("provider", "sops"))]'
          - gvk: apps/v1/Deployment
            name: sops-secrets-operator
            pick:
              - cel: '[refBuilder().incoming(ref("provider", "sops"))]'
```

This produces two refs per SopsSecret:

1. `SopsSecret → Secret` (id-based, reverse) — the SopsSecret creates the **Kubernetes** `Secret` in the same namespace
2. `SopsSecret → Deployment/sops-secrets-operator` (provider-based) — the SopsSecret needs the operator

Downstream **policy** that clones Secrets (for example Kyverno) continues to target **`v1/Secret`** resources; the `SopsSecret` remains visible in Hydra’s graph for provenance via this parser.

## Storage-Class and CSI Chains

Storage-backed workloads often depend on infrastructure that is not referenced directly from the workload manifest. Hydra models those dependencies as a chain of ordinary refs plus optional provider refs:

```text
StatefulSet
  ──volumeClaimTemplates[*].spec.storageClassName──▶ StorageClass
PersistentVolumeClaim
  ──spec.storageClassName──▶ StorageClass
PersistentVolume
  ──spec.storageClassName──▶ StorageClass
StorageClass
  ──provisioner──▶ CSIDriver
CSIDriver
  ──provider("...")──▶ CSI workload (chart-defined)
```

The first four edges are built into `hydra-go` and use ordinary `id(...)` endpoints:

- `StatefulSet.spec.volumeClaimTemplates[*].spec.storageClassName -> storage.k8s.io/v1/StorageClass`
- `PersistentVolumeClaim.spec.storageClassName -> storage.k8s.io/v1/StorageClass`
- `PersistentVolume.spec.storageClassName -> storage.k8s.io/v1/StorageClass`
- `StorageClass.provisioner -> storage.k8s.io/v1/CSIDriver`

The last edge is chart-defined because Kubernetes resources do not expose a native object reference from `CSIDriver` to the concrete controller or node workload. Charts that ship a CSI driver should therefore add provider parsers in `global.hydra.refs` so Hydra can connect the driver object to the workload that must run first.

For the NFS CSI driver this looks like:

```text
StatefulSet/activemq
  ──volumeClaimTemplates.storageClassName──▶ StorageClass/nfs-csi
  ──provisioner──▶ CSIDriver/nfs.csi.k8s.io
  ──provider("nfs.csi.k8s.io/node")──▶ DaemonSet/csi-nfs-node
```

Once those refs exist, `ResolveTransitiveWorkloadDeps` can derive a synthetic workload dependency from `StatefulSet/activemq` to `DaemonSet/csi-nfs-node`.

## Dependency Ordering

During `hydra gitops apply`, refs are used to determine the scale-up order of workloads.

### Pipeline

```text
Refs()
  → refs (direct entity-to-entity dependencies)
  → ResolveTransitiveWorkloadDeps(refs, workloadIds)
      → enriched refs (synthetic workload-to-workload via non-workload intermediaries,
        while preserving ref tags such as optional:startup)
  → split refs into required vs optional workload edges
  → BuildDependencyGraph(requiredWorkloadEntities, requiredRefs)
      → graph for the required startup part
  → TopologicalExecute(graph, ...)
      → ordered execution of required workloads
  → BuildDependencyGraph(optionalOnlyWorkloadEntities, optionalRefsWithinOptionalSet)
      → graph for optional-only workloads
  → TopologicalExecute(graph, ...)
      → ordered execution of optional workloads after the required part
```

### ResolveTransitiveWorkloadDeps

Workloads often depend on each other indirectly through non-workload resources (Secrets, ConfigMaps, SopsSecrets, etc.). `ResolveTransitiveWorkloadDeps` traces these chains via BFS and creates synthetic `indirect` refs between the workloads.

Synthetic refs inherit the semantic tags of the traversed path. In particular, if any path segment is tagged `optional:startup`, the resulting synthetic workload ref is also treated as optional for startup planning.

### Reverse Flag

When `Reverse=true` on a Ref, the dependency direction is inverted: `To` depends on `From` (instead of `From` depends on `To`). `BuildDependencyGraph` swaps `from`/`to` before adding the edge.

Example: `SopsSecret → Secret` with `Reverse=true` means "Secret depends on SopsSecret" (because the SopsSecret creates the Secret).

### Full Example: dex depends on sops-secrets-operator

```text
Deployment/dex
  ──imagePullSecrets──▶ Secret/dex/image-pull-secret
    ├──clone-source──▶ Secret/sops-secrets-operator/image-pull-secret
    │   ◀──label sops + attribute origin:generated=controller (reverse)── SopsSecret/sops-secrets-operator/image-pull-secret
    │     ──ref("provider","sops")──▶ Deployment/sops-secrets-operator
    └──ref("provider","kyverno")──▶ Deployment/kyverno-admission-controller
```

`ResolveTransitiveWorkloadDeps` follows this chain and creates:
`Deployment/dex → (indirect) → Deployment/sops-secrets-operator`
`Deployment/dex → (indirect) → Deployment/kyverno-admission-controller`

This ensures the source secret can exist in the operator namespace and that Kyverno is running before dex starts with a cloned `image-pull-secret`.

### No App-Wide Workload Fanout

Hydra no longer creates synthetic workload edges by saying "if any entity in App A references any entity in App B, then every workload in App A depends on every workload in App B".

Instead, only concrete dependency paths are considered:

- direct workload refs
- transitive workload refs through concrete non-workload entities
- provider-based refs that resolve to concrete operator workloads

This avoids over-linking unrelated workloads across apps while still preserving real chains such as `SopsSecret -> Secret -> cloned Secret -> Deployment`.

### Infrastructure Dependency Chains

The Strimzi and ClickHouse parsers model multi-level dependency chains that connect service Deployments to infrastructure operators through CRs and operator-generated Secrets.

**Strimzi (Kafka):**

```text
Deployment/service-X
  ──envFrom──▶ Secret/demo/kafkauser-service-X  (missing entity, created by operator)
    ◀──kafkauser (reverse)── KafkaUser/demo/service-X
      ──kafka-cluster──▶ Kafka/demo/demo-kafka  (via strimzi.io/cluster label)
        ──ref("provider","strimzi")──▶ Deployment/strimzi-cluster-operator
```

`ResolveTransitiveWorkloadDeps` follows this chain and creates:
`Deployment/service-X → (indirect) → Deployment/strimzi-cluster-operator`

This ensures the Kafka cluster is fully operational before services that depend on it start.

## Data Flow

```text
Entities (from helm rendering)
  │
  ▼
references.Refs(l, entities, key, extraParsers...)
  │
  ├── Load embedded ref-parser YAML files
  ├── Load app-defined ref-parsers from HydraValues.Refs (with YAML-level tag/desc/label defaults)
  ├── Create CEL env with CRDs list and service info
  ├── Compile CEL predicates and pick expressions
  ├── For each entity: evaluate matching parsers → RefDefinitions (with merged tags/desc/label)
  ├── Build incoming/outgoing index maps
  ├── Connect endpoints → Ref objects (including provider-based operator dependencies)
  ├── Create refs to missing entities (outgoing "id" refs without incoming match)
  └── Merge: labels union, tags union, desc/label from first
  │
  ▼
([]Ref, error)
  │
  ├── Cluster inventory consumers may run CanonicalizeOwnerRefTargetsToClusterIDs (owner-ref version skew)
  ├── Consumers that need materialized Secret/ConfigMap presence may apply recursive **`origin:generated`** expansion to a fixpoint (see [Generated targets, recursive evaluation, and fixpoint](#generated-targets-recursive-evaluation-and-fixpoint))
  ├── view.ToModel() → uses refs for grouping and DependenciesModel
  ├── Apply ordering → ResolveTransitiveWorkloadDeps → BuildDependencyGraph → TopologicalExecute
  └── Hydra UI → visualizes as edges in the dependency graph (with tags and dummy nodes)
```

**Note:** Planned test cases for ref metadata, provenance, and parser conventions are documented under [Test Structure > Tests to add or extend](#tests-to-add-or-extend-metadata-provenance-and-consumers) (single checklist; not duplicated here).
