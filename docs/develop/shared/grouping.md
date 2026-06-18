# Entity Grouping Algorithm

Entity groups determine which Kubernetes resources belong together in a logical group (e.g. a Deployment with its Service, ConfigMap, and RBAC chain). Groups are computed by `computeGroups()` and used by the Hydra UI for visual clustering in the dependency graph.

## Key Concepts

- **Group seeds** — workloads (Deployment, StatefulSet, etc.) and standalone ServiceAccounts that start their own group
- **Standalone SA detection** — a ServiceAccount is a standalone seed only when no workload references it (zero non-reverse incoming edges)
- **Logical edge direction** — the `reverse` flag on references inverts logical direction (e.g. RoleBinding→SA with reverse means SA logically owns the binding)
- **Phase 1a: Seed absorption** — seeds iteratively absorb neighboring non-seed entities with no external incoming or outgoing edges
- **Phase 1b: Seed merging** — singleton seeds with identical neighbor fingerprints are merged, then re-absorption runs
- **Phase 2: Union-find merging** — remaining ungrouped entities are organized via union-find; degree-1 non-seed groups are collapsed into connected seed groups
- **Phase 3: Shared leaves** — isolated leaf entities referenced by multiple groups are merged into a "Shared" group
- **Group naming** — priority: workload name > standalone SA name > single leaf entity name > "Shared"

**Source file:** `core/view/dependencies.go`

→ **Full details:** [details/grouping.md](details/grouping.md)
