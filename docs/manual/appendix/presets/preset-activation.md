# Preset Activation

How presets are activated, chained, and debugged.

## Activation Logic

A preset is **active** if:

1. Its `enabled` override is `true`, OR
2. Its embedded `defaultEnabled` is `true` and no override sets `enabled: false`, OR
3. Another active preset's `activates` list includes it and the target preset was not explicitly disabled

A preset is **inactive** if:

1. Its `enabled` override is `false`, OR
2. It is not `defaultEnabled` and no active preset activates it, OR
3. It was only activated transitively and another active preset excludes it with `!<preset-id>`

An explicit `enabled: false` wins over activation chains: other presets cannot turn that preset back on. An explicit `enabled: true` wins over `defaultEnabled: false`, but it must still be compatible with active exclusions.

## Activation Chains

Presets can activate other presets:

```yaml
presets:
  talos:
    enabled: true
    activates:
      - coredns       # Also enable coredns
      - "!flannel"    # Do not activate flannel through this chain
```

`activates` is resolved as a fixpoint, so chains are transitive: if A activates B, and B activates C, enabling A also enables C.

Entries with `!` are exclusions. They block automatic/transitive activation of that target. If the excluded target is still active because it is `defaultEnabled` or explicitly set to `enabled: true`, Hydra rejects the configuration instead of silently disabling it. Disable that target directly with `enabled: false` when you want it off.

Activation can also be gated by the Kubernetes minor version:

```yaml
presets:
  kubernetes:
    activates:
      - preset: kubernetes-volume-attributes-class
        kubernetesMinorMin: 34
```

The map form supports:

| Field | Meaning |
|-------|---------|
| `preset` | Builtin preset id to activate or exclude |
| `exclude` | Set to `true` to express the same exclusion as `!<preset-id>` |
| `kubernetesMinorMin` | Apply only on Kubernetes minor versions greater than or equal to this value |
| `kubernetesMinorMax` | Apply only on Kubernetes minor versions less than or equal to this value |

The string form remains supported for simple cases:

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

## Enabling and Disabling Presets

Put preset overrides under `global.hydra.presets` in values:

```yaml
global:
  hydra:
    presets:
      cloud-poc:
        enabled: true
      kube-proxy:
        enabled: false
```

Use `enabled: true` to force-enable a builtin preset. Use `enabled: false` to force-disable it and prevent activation chains from re-enabling it. Omit `enabled` or set it to `null` to use the preset's embedded `defaultEnabled`.

Preset keys are builtin preset ids. Hydra validates unknown ids when it builds the effective preset set.

## Inspecting Active Presets

```bash
# Show active presets and their matches against live cluster
hydra gitops system prod

# Show ALL presets (including inactive) with match status
hydra gitops system prod --all
```

The output shows:

- Which presets are enabled
- Which predicates match live resources
- Which predicates have unmatched resources (potential configuration issues)

## Debugging: Why a Preset Doesn't Match

If `hydra gitops system` shows a preset as active but resources remain untracked:

1. **Check predicate IDs** — Is the resource ID exactly right? (GVK + namespace + name must match perfectly)
2. **Check CEL expressions** — Does the pattern cover the actual resource names?
3. **Check version gates** — Is `kubernetesMinorMin`/`kubernetesMinorMax` blocking the predicate?
4. **Check optional markers** — Optional predicates don't flag as errors when unmatched

### Useful Debug Commands

```bash
# List all live resource IDs
hydra gitops list prod

# Dump a specific resource to see its exact GVK
hydra gitops dump prod | grep "my-resource"

# Check which resources are untracked
hydra gitops untracked prod
```

## Debugging: Unexpected Untracked Resources

If resources show as untracked:

1. Check if a preset should cover them → enable or add predicates
2. Check if an app should own them → add refs in values
3. Check if they are truly orphaned → manual cleanup needed

## See Also

- [Preset Overrides](preset-overrides.md)
- [Builtin Presets](builtin-presets.md)
- [Commands: cluster system](../commands/cluster/system.md)
- [Commands: cluster untracked](../commands/cluster/untracked.md)
