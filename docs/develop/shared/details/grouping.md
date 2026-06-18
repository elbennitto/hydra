# Entity Grouping Algorithm

## Overview

Entity groups are computed by `computeGroups()` in `core/view/dependencies.go`. The algorithm determines which Kubernetes resources belong together in a logical group (e.g. a Deployment with its Service, ConfigMap, ServiceAccount, and RBAC chain). Groups are used by the Hydra UI for visual clustering in the dependency graph.

**Source file:** `core/view/dependencies.go`

The UI receives the groups as a flat list of `{ name, ids[] }` entries and uses them for tree construction (see [hydra-ui layout.md](../../hydra-ui/details/layout.md)).

## Concepts

### Group Seeds

A **group seed** is an entity that creates its own group and absorbs connected non-seed entities. Seeds are never absorbed into other groups. There are two types:

| Type                          | GVK Examples                                                      | Condition                                                                                                           |
| ----------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| **Workload**                  | Deployment, StatefulSet, DaemonSet, ReplicaSet, Pod, Job, CronJob | Always a seed                                                                                                       |
| **Standalone ServiceAccount** | ServiceAccount                                                    | Only a seed when it has **no non-reverse incoming edges** (i.e. no workload references it via `serviceAccountName`) |

A ServiceAccount that IS referenced by a workload is NOT a standalone seed — it gets absorbed into the workload's group instead.

### Logical Edge Direction

References have a `reverse` flag that inverts their logical direction:

| Raw reference | reverse | Logical outgoing from | Logical incoming to |
| ------------- | ------- | --------------------- | ------------------- |
| `A → B`       | `false` | A                     | B                   |
| `A → B`       | `true`  | B                     | A                   |

**Example:** A RoleBinding has a `subject` reference to a ServiceAccount with `reverse: true`. This means the SA has a logical outgoing edge to the RoleBinding (the SA "owns" the binding), even though the raw YAML reference points from RoleBinding to SA.

### Standalone SA Detection

A ServiceAccount becomes a standalone seed when it has **zero non-reverse incoming edges**. This means no workload uses it via `serviceAccountName` (which would be a non-reverse edge `Deployment → SA`). Instead, it is only connected via:

- **Reverse edges from RoleBindings/ClusterRoleBindings** (subjects) — these become logical outgoing from the SA
- **Non-reverse outgoing to Secrets** (imagePullSecrets) — these are also outgoing from the SA

Such a SA acts as the "root" of its own RBAC chain.

## Algorithm Phases

### Phase 1a — Seed Absorption

Each group seed (workload or standalone SA) iteratively absorbs neighboring non-seed entities into its group. An entity is absorbed when:

- It has **no external incoming edges** (all incoming edges come from within the seed's group), OR
- It has **no external outgoing edges** (all outgoing edges go to entities within the seed's group)

The absorption runs iteratively: after absorbing one entity, the group checks again for new candidates, since the newly absorbed entity's neighbors may now also qualify.

**Entities that are themselves seeds are never absorbed** — they keep their own groups.

```text
Example: Deployment → SA ←(reverse)─ RB → Role

1. Deployment is a workload seed → starts group {Deployment}
2. SA has 1 incoming from Deployment (in-group), 1 outgoing to RB (external)
   → externalIncoming=0, incomingFromGroup=1 → absorbed → {Deployment, SA}
3. RB has 1 incoming from SA (now in-group), 1 outgoing to Role (external)
   → externalIncoming=0, incomingFromGroup=1 → absorbed → {Deployment, SA, RB}
4. Role has 1 incoming from RB (now in-group), 0 outgoing
   → externalIncoming=0, incomingFromGroup=1 → absorbed → {Deployment, SA, RB, Role}
```text

### Phase 1b — Seed Merging (Identical Fingerprints)

After Phase 1a, some seeds may still be singletons (only the seed itself in the group, nothing absorbed). If two or more singleton seeds share the **exact same set of logical neighbors** (same fingerprint of connected entity IDs), they are merged into a single group.

After merging, absorption (Phase 1a) is re-run for the combined group — the merged seeds may now be able to absorb shared dependencies that were previously blocked.

```text
Example: Two Jobs referencing the same SA

  Job-create → SA ←(reverse)─ RB → Role
  Job-patch  → SA ←(reverse)─ RB → Role

Phase 1a: Neither Job can absorb SA (SA has 2 external incoming from both Jobs)
Phase 1b: Both Jobs have fingerprint {SA} → merged into one group {Job-create, Job-patch}
Re-absorption: SA now has 0 external incoming → absorbed, then RB, then Role
Final: {Job-create, Job-patch, SA, RB, Role}
```

### Phase 2 — Union-Find Degree-1 Merging

Remaining ungrouped entities (not absorbed by any seed) are organized using a union-find structure. The algorithm iteratively merges groups that:

1. Have **degree 1** (connected to exactly one other group)
2. Do **not contain a seed** (workload or standalone SA)

This collapses linear chains of non-seed entities into their connected seed group.

Additionally, **shared leaves** (groups with inDegree > 1 and outDegree = 0) are merged together into a single "Shared" group.

### Phase 3 — Ungrouped Shared Leaves

Individual entities that remain ungrouped (group size = 1) and have inDegree > 1 and outDegree = 0 are merged into a single "Shared" group. This catches isolated leaf entities referenced by multiple groups.

## Group Naming

Groups are named based on priority:

| Priority | Condition                                     | Name format                                         |
| -------- | --------------------------------------------- | --------------------------------------------------- |
| 1        | Group contains a workload                     | `"name (Kind)"` e.g. `"nginx (Deployment)"`         |
| 2        | Group has a standalone SA seed                | `"name (Kind)"` e.g. `"admission (ServiceAccount)"` |
| 3        | Group has no seed but exactly one leaf entity | Entity's name                                       |
| 4        | All other multi-entity groups                 | `"Shared"`                                          |

## DependenciesModel

The final output combines entities, groups, and references:

```go
type DependenciesModel struct {
    Entities   []IdModel    `yaml:"entities"`
    Groups     []GroupModel `yaml:"groups"`
    References []RefModel   `yaml:"references"`
}

type IdModel struct {
    Id            types.Id        `yaml:"id"`
    Tags          []string        `yaml:"tags,omitempty"`
    TemplatePath  string          `yaml:"templatePath,omitempty"`
    TemplateIndex int             `yaml:"templateIndex,omitempty"`
    RbacRules     []RbacRuleModel `yaml:"rbacRules,omitempty"`
    SecretKeys    []string        `yaml:"secretKeys,omitempty"`
}

type RbacRuleModel struct {
    ApiGroups     []string `yaml:"apiGroups"`
    Resources     []string `yaml:"resources"`
    Verbs         []string `yaml:"verbs"`
    ResourceNames []string `yaml:"resourceNames,omitempty"`
}

type GroupModel struct {
    Name string     `yaml:"name,omitempty"`
    Ids  []types.Id `yaml:"ids"`
}

type RefModel struct {
    From    types.Id `yaml:"from"`
    To      types.Id `yaml:"to"`
    Labels  []string `yaml:"labels,omitempty"`
    Reverse bool     `yaml:"reverse,omitempty"`
}
```text

**IdModel enrichment:** For Role and ClusterRole entities, RBAC policy rules are extracted and included as `RbacRules`. For Secret entities, data/stringData key names are included as `SecretKeys`. For Secrets created by SopsSecrets, the key names are extracted from the SopsSecret's `secretTemplates`.

### Model Building Pipeline

```go
func ToModel(l log.Logger, entities entity.Entities) (DependenciesModel, error)
```

```text
Entities
  │
  ▼
1. Collect all entity IDs
  │
  ▼
2. references.Refs(l, entities, KeyTemplateEntity) → refs
  │  Discover all references between entities
  │
  ▼
3. Identify missing entities (outgoing refs to non-existing IDs)
  │  Tag with "app:missing" or "controller:sops-secrets-operator" (for SopsSecret-produced Secrets)
  │
  ▼
4. computeGroups(allIds, refs) → groups
  │  Phase 1a: Seed absorption
  │  Phase 1b: Seed merging
  │  Phase 2: Union-find degree-1 merging
  │  Phase 3: Ungrouped shared leaves
  │
  ▼
5. Build IdModels (with template source info, RBAC rules, secret keys)
  │  Only groups with >1 entity are included
  │
  ▼
6. Build GroupModels
  │
  ▼
7. Build RefModels
  │
  ▼
8. Sort all components for deterministic output
  │
  ▼
DependenciesModel
```text

### Rendering to YAML

```go
func RenderDependencies(l log.Logger, w io.Writer, entities entity.Entities) error
```

Calls `ToModel` internally and marshals the resulting `DependenciesModel` to YAML, writing directly to the provided `io.Writer`. The result is the `.hydra.yaml` file consumed by the Hydra UI. See [hydra-yaml.md](hydra-yaml.md) for the full file format documentation.

## Examples

### Workload absorbs SA + RBAC chain

```text
Input:  Deployment/nginx → SA/nginx-sa ←(rev)─ RB/nginx-binding → Role/nginx-role
Result: Group "nginx (Deployment)" = {Deployment, SA, RoleBinding, Role}
```text

### Standalone SA absorbs RBAC chain

```text
Input:  SA/admission ←(rev)─ RB/admission → Role/admission
                     ←(rev)─ CRB/admission → CR/admission
                     → Secret/image-pull-secret
No workload references the SA.

Result: Group "admission (ServiceAccount)" = {SA, RB, Role, CRB, CR, Secret}
```

### SA with workload is NOT a standalone seed

```text
Input:  Deployment/nginx → SA/nginx-sa ←(rev)─ RB/nginx-binding → Role/nginx-role
SA has non-reverse incoming edge from Deployment → NOT a standalone seed.

Result: Group "nginx (Deployment)" = {Deployment, SA, RoleBinding, Role}
        (SA is absorbed by the Deployment's group, not its own)
```text

### Two Jobs with same dependencies → merged

```text
Input:  Job/admission-create → SA/admission ←(rev)─ RB → Role
        Job/admission-patch  → SA/admission ←(rev)─ CRB → CR

Phase 1a: Neither Job absorbs SA (2 external incoming)
Phase 1b: Both Jobs have fingerprint {SA} → merged
Re-absorption: SA absorbed, then RB+Role and CRB+CR
Result: Group "admission-create (Job)" = {Job-create, Job-patch, SA, RB, Role, CRB, CR}
```

## Test Cases

Test data is in `core/view/testdata/kubernetes/`. Each test has a `.given.yaml` (input) and `.expected.yaml` (golden file output). Run with `go test ./core/view/ -run TestDependenciesModelParameterized`.

| Test file | Scenario |
| --- | --- |
| `simple` | Basic entity grouping |
| `standalone_sa_rbac_chain` | SA without workload → own group with full RBAC chain + Secret |
| `sa_with_workload_not_seed` | SA with Deployment → absorbed into Deployment group (NOT a seed) |
| `jobs_same_dependencies` | Two Jobs with identical deps → merged into one group |
| `jobs_different_dependencies` | Two Jobs with different deps → separate groups |
| `pod_serviceaccount` | Pod + SA + RBAC chain → single workload group |
| `service_deployment_rbac` | Deployment + Service + SA + RBAC → single workload group |
| `argocd_repo_server_services` | Two Deployments with shared ConfigMaps → merged + shared group |
| `shared_secret_multiple_sources` | Secret referenced by multiple Deployments → separate groups + shared |
| `generated_controller_secret` | SopsSecret→Secret (`origin:generated`: controller); `controller:sops-secrets-operator` vs `app:missing` |

**Update golden files:**

```bash
./hydra-go/update_testdata.sh
# or: go test -count=1 ./core/view/... -update
```
