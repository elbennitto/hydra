# Clones and TemplatePatches

Runtime resource copying and post-render mutations.

## Clones

### What Are Clones?

Clones copy resources from one location to another at rendering time. The typical use case is distributing image-pull-secrets or TLS certificates across multiple namespaces.

### How They Work

1. A **predicate** (CEL expression) selects source resources to clone
2. **Targets** define where copies should be placed
3. **Exclude** patterns prevent specific resources from being cloned
4. An optional **tag** (e.g., `bootstrap`) controls when clones are evaluated

### Example: Image Pull Secret Distribution

A Kyverno ClusterPolicy clones an image-pull-secret into every namespace. In Hydra values:

```yaml
global:
  hydra:
    clones:
      image-pull-secret:
        tag: bootstrap
        predicate: 'kind == "Secret" && name == "registry-credentials"'
        targets:
          - namespace: "*"
        exclude:
          - 'ns == "kube-system"'
```

### When Clones Are Evaluated

Clones are evaluated during rendering. The resulting copied resources appear in the rendered output and are managed like any other resource (including refs and uninstall).

## TemplatePatches

### What Are TemplatePatches?

TemplatePatches are YQ-based mutations applied **after** Helm rendering but **before** ref parsing. They modify rendered manifests without changing the Helm templates themselves.

### Structure

```yaml
global:
  hydra:
    templatePatches:
      add-sync-annotation:
        predicate: 'kind == "Application"'
        patches:
          - '.metadata.annotations["argocd.argoproj.io/sync-options"] = "SkipDryRunOnMissingResource=true"'
```

- **predicate** — CEL expression selecting which resources to patch
- **patches** — List of YQ expressions to apply

### Common Use Cases

| Pattern | Purpose |
|---------|---------|
| ArgoCD sync annotations | Add sync options to all Applications |
| Label injection | Add standard labels to all resources |
| Resource limits | Override resource limits per cluster |

### Order of Application

TemplatePatches are applied:
1. After Helm template rendering
2. Before ref parsing
3. In the order they appear in values

## See Also

- [Values: Clones](../values/clones-in-values.md)
- [Values: TemplatePatches](../values/template-patches.md)
- [CEL](../cel/)
