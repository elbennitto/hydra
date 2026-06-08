# Preset Overrides

For cluster-specific manual app assignment via `hydra.yaml`, see [Cluster Overrides](../configuration/cluster-overrides.md).

Customizing preset behavior through `global.hydra.presets` in values.

## Override Structure

```yaml
global:
  hydra:
    presets:
      <preset-id>:
        enabled: true|false|null
        activates:
          - "<other-preset>"
          - "!<excluded-preset>"
          - preset: "<other-preset>"
            exclude: false
            kubernetesMinorMin: <int>
            kubernetesMinorMax: <int>
        predicates:
          <group-name>:
            enabled: true|false|null
            ids:
              - '<resource-id>'
            cel:
              - cel: '<CEL expression>'
                optional: true|false
            kubernetesMinorMin: <int>
            kubernetesMinorMax: <int>
```

## Enable/Disable a Preset

```yaml
presets:
  talos:
    enabled: true
  flannel:
    enabled: false
```

- `true` — Force enable regardless of `defaultEnabled`
- `false` — Force disable regardless of `defaultEnabled` and block activation chains from enabling it
- `null` (or omitted) — Use the preset's `defaultEnabled`

## Add Custom Predicates

Add extra resources that the builtin preset doesn't cover:

```yaml
presets:
  talos:
    enabled: true
    predicates:
      custom-daemonset:
        ids:
          - 'apps/v1/DaemonSet/kube-system/talos-custom-agent'
      custom-pods:
        cel:
          - cel: 'gvk == "v1/Pod" && ns == "kube-system" && name.startsWith("talos-")'
            optional: true
```

Override predicates are **merged** with builtin predicates — they add to the existing set, they do not replace it.

## Disable a Predicate Group

Disable a specific predicate group from a builtin preset:

```yaml
presets:
  kubernetes:
    predicates:
      deprecated-resource:
        enabled: false
```

## Activation Chains

Control other presets from an override:

```yaml
presets:
  cloud-poc:
    enabled: true
    activates:
      - gardener
      - calico
      - cinder
      - metrics-server
```

Activation chains are transitive. A preset activated by another active preset can activate more presets.

The `!` prefix excludes a preset from automatic activation:

```yaml
presets:
  gardener:
    activates:
      - "!kube-proxy"
      - preset: cinder-controller
        exclude: true
```

This is not a silent force-disable. If the excluded preset is already active because of `defaultEnabled` or `enabled: true`, Hydra reports a configuration error. To switch a preset off for the cluster, set that preset to `enabled: false`.

Activation entries can also be version-gated:

```yaml
presets:
  kubernetes:
    activates:
      - preset: kubernetes-dynamic-resource-allocation
        kubernetesMinorMin: 34
```

## Where to Define Overrides

| Scope | Location | Use case |
|-------|----------|----------|
| All clusters | Charts repo `values.yaml` | Preset that always applies for this app |
| One cluster | GitOps repo cluster `values.yaml` | Cluster-specific distribution |

Typically, preset overrides live in the **GitOps repository** because they depend on the Kubernetes distribution, which varies per cluster.

## See Also

- [Builtin Presets](builtin-presets.md)
- [Preset Activation](preset-activation.md)
- [Values: presets](../values/presets-in-values.md)
