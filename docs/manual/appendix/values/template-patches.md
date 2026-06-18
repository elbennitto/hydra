# templatePatches in Values

`templatePatches` are YQ-based mutations for rendered Kubernetes manifests. Use them for small, explicit workarounds or normalization steps when changing the upstream chart or dependency is not desirable.

Patches can be declared in an app's Helm values under `global.hydra.templatePatches` or in a Hydra configuration `ConfigMap` (`hydra-gitops.org/hydra-config: "true"`) under `data.hydra.templatePatches`.

## Structure

```yaml
global:
  hydra:
    templatePatches:
      <patch-name>:
        predicate: '<CEL>'         # Which resources to patch
        patches:
          - yq: '<yq-expression>'  # YQ mutation to apply
```

## Example: ArgoCD Sync Annotations

```yaml
global:
  hydra:
    templatePatches:
      argocd-sync-options:
        predicate: 'kind == "Application"'
        patches:
          - yq: '.metadata.annotations."argocd.argoproj.io/sync-options" = "SkipDryRunOnMissingResource=true"'
```

## Example: Inject Labels

```yaml
global:
  hydra:
    templatePatches:
      team-label:
        predicate: 'ns == "demo"'
        patches:
          - yq: '.metadata.labels.team = "platform"'
```

## Example: Remove an Invalid Namespace from a Cluster-Scoped Resource

Some charts render `metadata.namespace` on cluster-scoped resources such as `PriorityClass`, `ClusterRole`, or CRDs. Kubernetes rejects those manifests. A template patch can remove the invalid field before Hydra validates resource scope:

```yaml
global:
  hydra:
    templatePatches:
      priorityClassNoNamespace:
        # Workaround for an upstream chart rendering a namespace on a cluster-scoped resource.
        # Remove this once the dependency is fixed.
        predicate: 'gvk == "scheduling.k8s.io/v1/PriorityClass"'
        patches:
          - yq: 'del(.metadata.namespace)'
```

## Fields

### predicate

CEL expression selecting which rendered resources receive the patch. It uses the same resource variables as ref predicates, for example `kind`, `name`, `ns`, `gvk`, `id`, and `entity`.

### patches

List of YQ expressions applied sequentially to each matching resource. Each expression mutates the resource's YAML structure and should be idempotent.

## Application Order

1. Helm renders templates
2. Hydra collects template patch rules from app values and Hydra config ConfigMaps
3. Template patches apply once before scope validation
4. Hydra validates and propagates resource scope, including namespaced vs cluster-scoped resources
5. Cluster-aware commands normalize preferred `apiVersion` values where discovery provides them
6. Template patches apply again as the final template-side mutation
7. Ref parsers analyze the patched output and the dependency graph is built

The early pass lets a patch fix invalid raw chart output, for example deleting `metadata.namespace` from a cluster-scoped resource before scope validation. The final pass preserves the older behavior where patches affect the printed or applied manifest set after normalization.

Patches must not change resource identity in the final pass: `apiVersion`, `kind`, `metadata.name`, and `metadata.namespace` must remain stable. The early pre-scope pass is the only exception for `metadata.namespace`, because it exists specifically to let patches remove invalid namespaces before Hydra decides resource scope.

Hydra config ConfigMaps are protected. A template patch must not mutate a ConfigMap marked with `hydra-gitops.org/hydra-config: "true"` and containing `data.hydra`.

## Common Patterns

| Pattern | Purpose |
|---------|---------|
| Add annotations | `.metadata.annotations.key = "value"` |
| Add labels | `.metadata.labels.key = "value"` |
| Override field | `.spec.replicas = 3` |
| Delete field | `del(.spec.template.metadata.annotations.key)` |
| Remove invalid namespace | `del(.metadata.namespace)` |

## See Also

- [Concepts: Clones and TemplatePatches](../concepts/clones.md)
- [CEL resource filters](../commands/README.md#cel-resource-filters)
