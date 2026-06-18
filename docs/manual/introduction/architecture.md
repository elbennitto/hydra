# Architecture

## System Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│ Charts Repo     │     │  GitOps Repo     │     │  Cluster        │
│                 │     │                  │     │                 │
│ apps/           │     │ clusters/        │     │ Live K8s State  │
│   Chart.yaml    │────>│   values.yaml    │────>│                 │
│   templates/    │     │   secrets/       │     │ ArgoCD          │
│   values.yaml   │     │   backups/       │     │ Applications    │
└─────────────────┘     └──────────────────┘     └─────────────────┘
        │                        │                        ▲
        │                        │                        │
        ▼                        ▼                        │
┌──────────────────────────────────────────┐              │
│              Hydra CLI                   │              │
│                                          │──────────────┘
│  Render → Diff → Apply / Uninstall       │
│  Refs → Graph → Ordered Operations       │
│  Presets → Cluster Inventory Matching    │
└──────────────────────────────────────────┘
```

## Rendering Pipeline

1. **Helm Template** — Render charts with merged values (charts-repo base + gitops-repo overrides + Hydra ConfigMaps)
2. **Ref Parsing** — Apply CEL-based ref-parsers to discover dependency edges between rendered resources
3. **Virtual Materialization** — Recursively generate virtual resources from ref targets until fixpoint
4. **Preset Matching** — Match cluster inventory against preset predicates to identify infrastructure components
5. **Graph Construction** — Build the complete dependency graph with topological ordering

## The Dependency Graph

The dependency graph is Hydra's core data structure:

- **Nodes** = Kubernetes resources (identified by GVK + namespace + name)
- **Edges** = Refs (directed dependency relationships with type, label, tags, attributes)

The graph determines:
- Apply order (topological sort)
- Uninstall order (reverse topological sort)
- Backup scope (resources tagged `[backup]`)
- What belongs to which app

## Operating Modes

Hydra exposes three command surfaces with different sources of truth:

- **Local** (`hydra local *`) — Works only with rendered templates from the local Hydra context. No cluster connection is needed.
- **GitOps** (`hydra gitops *`) — Uses the local Hydra context as the source of truth and also connects to the Kubernetes API to diff, validate, or apply that rendered state.
- **Cluster** (`hydra cluster`) — Reserved stub for future cluster-only workflows where the local Hydra context is not available and Hydra ConfigMaps in the cluster become the source of truth. Use `hydra gitops` for current local-plus-cluster operations.

For the current `hydra gitops` workflows, the inventory merges:
- Rendered templates from the local Hydra context (desired state)
- Live resources from the API (actual state)
- Preset-matched resources (cluster defaults)
