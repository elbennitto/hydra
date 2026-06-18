# refs in Values

Defining dependency edges between Kubernetes resources.

## Structure

```yaml
global:
  hydra:
    refs:
      <group-name>:
        tag: [<tag>, ...]           # Tags applied to all refs in this group
        priority: 0                 # Optional default ownership priority for all ref-parsers in this group
        label: <string>             # Label applied to all refs
        ref-parsers:
          - predicate: '<CEL>'      # CEL predicate selecting source resources
            ids:                    # Static ID matches (alternative to CEL)
              - '<resource-id>'
```

## Ref Groups

Refs are organized into named groups. Each group defines a set of dependency edges from matching resources.

### Example: Event Cleanup

```yaml
refs:
  events:
    tag: [uninstall]
    ref-parsers:
      - predicate: 'gvk == "events.k8s.io/v1/Event" && ns == "cert-manager"'
```

This creates refs to all Events in the cert-manager namespace and tags them for removal during uninstall.

### Example: Backup Secrets

```yaml
refs:
  backup-tls:
    tag: [backup]
    ref-parsers:
      - predicate: 'kind == "Secret" && ns == "cert-manager" && name == "ca-key-pair"'
```

This marks the CA key secret for inclusion in backups.

### Example: CRD Management

```yaml
refs:
  crds:
    tag: [uninstall]
    ref-parsers:
      - predicate: 'gvk == "apiextensions.k8s.io/v1/CustomResourceDefinition" && name.startsWith("cert-manager")'
```

## Available Tags

| Tag | Effect |
|-----|--------|
| `[backup]` | Include in `cluster backup create` |
| `[uninstall]` | Remove during `cluster uninstall`; also assigns cluster-only resources to the matching app when unambiguous |
| `[uninstall-force]` | Show as force-deletable during `cluster uninstall`; delete with `--force` or skip with `--keep` |
| `[uninstall-safe]` | Safe to remove without impact |

See [Refs: Ref Tags](../refs/ref-tags.md) for the complete list.

## CEL Predicates in Refs

The `predicate` field uses CEL expressions. Available variables:

- `gvk` — Full GVK string (e.g., `"apps/v1/Deployment"`)
- `kind` — Kind only (e.g., `"Deployment"`)
- `name` — Resource name
- `ns` — Namespace
- `id` — Full resource ID

See [CEL: Variables](../cel/variables.md) for all available variables.

## Static ID Matches

Instead of CEL predicates, you can match by exact resource ID:

```yaml
refs:
  namespace:
    tag: [uninstall]
    ref-parsers:
      - ids:
          - 'v1/Namespace//cert-manager'
```

## Common Patterns

| Pattern | Predicate |
|---------|-----------|
| All Events in a namespace | `gvk == "events.k8s.io/v1/Event" && ns == "<ns>"` |
| Specific Secret for backup | `kind == "Secret" && ns == "<ns>" && name == "<name>"` |
| All CRDs with prefix | `gvk == "apiextensions.k8s.io/v1/CustomResourceDefinition" && name.startsWith("<prefix>")` |
| All resources in namespace | `ns == "<ns>"` |

## See Also

- [Refs](../refs/) — Complete refs documentation
- [CEL: Predicates](../cel/predicates.md)
