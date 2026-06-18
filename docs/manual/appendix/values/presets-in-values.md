# presets in Values

Overriding builtin preset configuration per app or cluster.

## Structure

```yaml
global:
  hydra:
    presets:
      <preset-id>:
        enabled: true|false|null     # Override default activation
        activates:                   # Other presets to activate
          - "<preset-id>"
          - "!<preset-id>"           # ! prefix = exclude from activation chains
          - preset: "<preset-id>"    # Map form, useful for version gates
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

## Enabling/Disabling Presets

```yaml
global:
  hydra:
    presets:
      cloud-poc:
        enabled: true      # Force-enable this preset
      kube-proxy:
        enabled: false     # Force-disable this preset
```

Set to `null` or omit `enabled` to use the preset's embedded `defaultEnabled` value. `enabled: false` also prevents other presets from re-enabling the preset through `activates`.

## Adding Custom Predicates

Add additional resource IDs or CEL expressions to an existing preset:

```yaml
presets:
  kubernetes:
    predicates:
      custom-controller:
        ids:
          - 'apps/v1/Deployment/kube-system/custom-controller'
        cel:
          - cel: 'kind == "DaemonSet" && ns == "kube-system" && name.startsWith("custom-")'
            optional: true
```

## Activation Chains

A preset can activate other presets:

```yaml
global:
  hydra:
    presets:
      cloud-poc:
        enabled: true
        activates:
          - gardener
          - calico
          - cinder
          - metrics-server
```

Activation is transitive: if `cloud-poc` activates `gardener`, and `gardener` activates `monex`, enabling `cloud-poc` also enables `monex`.

Use `!<preset-id>` or `exclude: true` to exclude a target from automatic activation:

```yaml
global:
  hydra:
    presets:
      gardener:
        activates:
          - "!kube-proxy"
          - preset: cinder-controller
            exclude: true
```

Exclusions are not the same as `enabled: false`: they block transitive activation and validate that incompatible presets are not active together. If the excluded preset is active by default or explicitly enabled, Hydra reports a configuration error. Set the target preset itself to `enabled: false` when you want to force it off.

## Version Gating

Restrict predicates or activation entries to specific Kubernetes versions:

```yaml
presets:
  kubernetes:
    activates:
      - preset: kubernetes-volume-attributes-class
        kubernetesMinorMin: 34
    predicates:
      new-feature:
        kubernetesMinorMin: 28    # Only on K8s 1.28+
        ids:
          - 'apps/v1/Deployment/kube-system/new-feature'
```

## See Also

- [Presets](../presets/) — Complete presets documentation
- [Presets: Builtin Presets](../presets/builtin-presets.md)
- [Presets: Overrides](../presets/preset-overrides.md)
