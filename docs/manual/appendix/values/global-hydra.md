# global.hydra Reference

Complete structure of the `global.hydra` values block.

## Top-Level Structure

```yaml
global:
  hydra:
    # Dependency modeling
    refs: {}                    # Ref group definitions
    presets: {}                 # Preset overrides
    clones: {}                  # Resource cloning rules
    templatePatches: {}         # Post-render YQ mutations

    # Operational configuration
    scale: {}                   # Workload scaling metadata
    diff: {}                    # Diff ignore rules
    ready: {}                   # Readiness probes
    ownerNamespaces: []         # Owned namespace list
    kubectl: {}                 # Kubernetes context validation
    uninstall-finalizer: []     # Custom uninstall ordering

    # Metadata
    cluster: ""                 # Cluster name
    path: ""                    # Chart path
    repository: ""              # Git repository URL
    revision: ""                # Git revision/branch
    stage: ""                   # Environment stage (dev/stage/prod)
    kubernetesVersion: ""       # Target K8s version
    additionalSourceRepos: []   # Extra ArgoCD source repos
```

## Keys by Purpose

### Dependency Modeling

| Key | Type | Purpose | Detail Page |
|-----|------|---------|-------------|
| `refs` | `map[string]RefGroup` | Define dependency edges | [refs](refs-in-values.md) |
| `presets` | `map[string]PresetOverride` | Override builtin presets | [presets](presets-in-values.md) |
| `clones` | `map[string]CloneSpec` | Copy resources across namespaces | [clones](clones-in-values.md) |
| `templatePatches` | `map[string]PatchSpec` | Mutate rendered manifests | [templatePatches](template-patches.md) |

### Operational Configuration

| Key | Type | Purpose | Detail Page |
|-----|------|---------|-------------|
| `scale` | `map[string]ScaleSpec` | Workload scaling metadata | [scale](scale.md) |
| `diff` | `map[string]DiffSpec` | Ignore rules for diff | [diff](diff.md) |
| `ready` | `map[string]ReadySpec` | CEL-based readiness | [ready](ready.md) |
| `ownerNamespaces` | `[]string` | Namespaces this app owns | [ownerNamespaces](owner-namespaces.md) |
| `kubectl` | `KubectlConfig` | Context validation | [kubectl](kubectl.md) |
| `uninstall-finalizer` | `[]string` | Uninstall ordering | [uninstall-finalizer](uninstall-finalizer.md) |

### Metadata

| Key | Type | Purpose |
|-----|------|---------|
| `cluster` | `string` | Name of the target cluster |
| `path` | `string` | Path to the chart in the repository (set at cluster `values.yaml`, not context `values.yaml`) |
| `repository` | `string` | Git repository URL |
| `revision` | `string` | Git branch or tag |
| `stage` | `string` | Environment stage identifier |
| `kubernetesVersion` | `string` | Target Kubernetes version (e.g., "1.28") |
| `additionalSourceRepos` | `[]string` | Extra repos for ArgoCD AppProject |

## See Also

- [Values: Overview](overview.md) — Merge order and semantics
