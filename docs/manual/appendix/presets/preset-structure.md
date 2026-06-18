# Preset Structure

The YAML format of preset definitions.

## Format

```yaml
id: <preset-name>
defaultEnabled: true|false
activates:
  - "<preset-id>"
  - "!<preset-id>"
  - preset: "<preset-id>"
    exclude: false
    kubernetesMinorMin: <int>
    kubernetesMinorMax: <int>
predicates:
  <group-name>:
    ids:
      - '<resource-id>'
    cel:
      - cel: '<CEL expression>'
        optional: true|false
    kubernetesMinorMin: <int>
    kubernetesMinorMax: <int>
```

## Fields

### id

The unique preset identifier. Used in `global.hydra.presets` for overrides.

### defaultEnabled

Whether the preset is active by default without explicit configuration. Core presets (like `kubernetes`, `coredns`) default to `true`; distribution-specific presets default to `false`.

### activates

Other builtin presets to activate when this preset is active. String entries activate another preset by id. Entries with `!` or `exclude: true` exclude a preset from transitive activation and validate that incompatible presets are not active together.

Activation entries can be gated with `kubernetesMinorMin` and `kubernetesMinorMax`.

### predicates

A map of named predicate groups. Each group matches a logically related set of resources.

## Predicate Groups

### ids

Static resource IDs to match exactly:

```yaml
predicates:
  deployment:
    ids:
      - 'apps/v1/Deployment/kube-system/coredns'
  serviceaccount:
    ids:
      - 'v1/ServiceAccount/kube-system/coredns'
```

### cel

CEL expressions for pattern-based matching:

```yaml
predicates:
  pod-metrics:
    cel:
      - cel: 'gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "kube-system" && name.matches("^coredns-.*")'
        optional: true
```

### optional

When `optional: true`, the predicate is evaluated but does not count toward the expected resource set during bootstrap audits. Use for resources that may or may not exist (e.g., PodMetrics only exist when metrics-server is running).

### kubernetesMinorMin / kubernetesMinorMax

Version gates that restrict a predicate group to specific Kubernetes versions:

```yaml
predicates:
  new-resource:
    kubernetesMinorMin: 28    # Only on K8s >= 1.28
    kubernetesMinorMax: 30    # Only on K8s <= 1.30
    ids:
      - 'apps/v1/Deployment/kube-system/new-feature'
```

## Example: Complete Preset

```yaml
id: coredns
defaultEnabled: true
predicates:
  deployment:
    ids:
      - 'apps/v1/Deployment/kube-system/coredns'
  service:
    ids:
      - 'v1/Service/kube-system/kube-dns'
  serviceaccount:
    ids:
      - 'v1/ServiceAccount/kube-system/coredns'
  configmap:
    ids:
      - 'v1/ConfigMap/kube-system/coredns'
  clusterrole:
    ids:
      - 'rbac.authorization.k8s.io/v1/ClusterRole//system:coredns'
  clusterrolebinding:
    ids:
      - 'rbac.authorization.k8s.io/v1/ClusterRoleBinding//system:coredns'
  pod-metrics:
    cel:
      - cel: 'gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "kube-system" && name.matches("^coredns-.*")'
        optional: true
```

## See Also

- [Builtin Presets](builtin-presets.md)
- [Preset Overrides](preset-overrides.md)
- [CEL: Variables](../cel/variables.md) — Variables available in presets
