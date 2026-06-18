# Hydra Context Architecture

The Hydra context represents the hierarchical structure of a GitOps repository. It defines how clusters, root applications, and child applications are organized on disk and how values are loaded and merged at each level.

## Key Concepts

- **Type hierarchy** — `Context → Cluster → RootApp → ChildApp`, all implementing the `Hydra` interface; apps additionally implement `HydraApp`
- **Path resolution** — `ResolvePath()` walks the directory tree upward to find the context root (contains `in-cluster/argocd/`) and determines the Hydra level
- **Values loading** — hierarchical deep-merge from context → cluster → root app → child app; each level adds its own `values.yaml`
- **AppId** — dot-separated identifier (`cluster.rootApp` or `cluster.rootApp.childApp`) that uniquely identifies an application
- **Template rendering** — loads Helm chart, merges values, renders via `helm.Template()`, applies ArgoCD annotations
- **Caching** — thread-safe lazy caches for charts, values, and templates to avoid redundant computation
- **HydraValues** — extracted configuration (allowed kubectl contexts, uninstall predicates, K8s version) used for validation
- **Kubernetes context validation** — prevents accidental operations against wrong clusters by checking `~/.kube/config`

**Source files:** `core/hydra/context.go`, `core/hydra/cluster.go`, `core/hydra/root_app.go`, `core/hydra/child_app.go`, `core/hydra/hydra.go`

→ **Full details:** [details/context.md](details/context.md)
