# Dependency Graph

How Hydra models relationships between Kubernetes resources.

## Why a Dependency Graph?

Kubernetes resources are not independent. A Deployment depends on its ServiceAccount, Namespace, and ConfigMaps. A Certificate depends on its Issuer. An Application depends on its target namespace existing.

Standard tools apply resources in file order or alphabetical order. Hydra builds a **directed acyclic graph** of dependencies and uses it to:

- Apply resources in the correct order
- Uninstall resources in reverse order
- Detect missing dependencies
- Visualize relationships

## Nodes and Edges

- **Node** = A Kubernetes resource identified by `<apiVersion>/<kind>/<namespace>/<name>` (its resource ID)
- **Edge** = A **ref** — a directed dependency from one resource to another

Example:
```
apps/v1/Deployment/default/myapp
  ──depends-on──▶ v1/ServiceAccount/default/myapp
  ──depends-on──▶ v1/Namespace/default/
  ──depends-on──▶ v1/ConfigMap/default/myapp-config
```

## Topological Ordering

Hydra sorts the graph topologically:

- **Apply order**: Dependencies first (Namespace → ServiceAccount → Deployment)
- **Uninstall order**: Dependents first (Deployment → ServiceAccount → Namespace)

This guarantees resources are created only after their dependencies exist and removed only after their dependents are gone.

## Edge Properties

Each ref (edge) carries metadata:

| Property | Purpose |
|----------|---------|
| Type | `direct`, `indirect`, `runtime`, `regarding` |
| Label | Semantic category (e.g., `namespace`, `crd`, `owner`) |
| Tags | Control behavior (e.g., `[backup]`, `[uninstall]`) |
| Attributes | Track origin (e.g., `origin:app`, `origin:owner`) |

See [Refs](../refs/) for the complete reference.

## How Edges Are Discovered

Edges are extracted by **ref-parsers** — CEL-based rules that scan rendered resources and extract dependency relationships. For example, a parser might extract all `ownerReferences` from a resource's metadata.

See [Refs: Ref Parsers](../refs/ref-parsers.md) for details.

## Visualizing the Graph

```bash
# Interactive TUI (local, no cluster needed)
hydra local inspect prod

# Interactive TUI (with live cluster state)
hydra gitops inspect prod

# Text output of transitive references
hydra local refs prod apps/v1/Deployment/default/myapp
```

## See Also

- [Refs](../refs/)
- [CEL](../cel/)
