# Ref Parsers

Ref-parsers are CEL-based rules that scan rendered Kubernetes resources and extract dependency edges.

## How Parsers Work

For each rendered resource (entity):

1. Evaluate each parser's **predicate** — Does this parser apply to this resource?
2. If the predicate matches: evaluate each **pick** rule — Extract ref definitions
3. Build ref edges from the extracted definitions

## Parser File Format

Parsers are defined in YAML files:

```yaml
ref-parsers:
  - predicate: '<CEL expression>'        # When to apply this parser
    priority: 0                           # Higher wins; negative values behave like weak fallback ownership
    tag: [<tag>, ...]                     # Tags for all refs from this parser
    label: '<label>'                      # Label for all refs
    reverse: false                        # Reverse edge direction?
    pick:
      - cel: '<CEL expression>'          # Extraction rule (returns []RefDefinition)
        label: '<override-label>'        # Override parser-level label
        attributes: [<key:value>, ...]   # Additional attributes
```

## Predicate

The `predicate` is a CEL expression that determines which resources this parser processes. It has access to all entity variables (`gvk`, `kind`, `name`, `ns`, etc.).

```yaml
# Match all resources
predicate: "true"

# Match only Deployments
predicate: 'gvk == "apps/v1/Deployment"'

# Match resources with ownerReferences
predicate: 'has(entity.metadata.ownerReferences) && entity.metadata.ownerReferences.size() > 0'
```

## The `priority` Field

`priority` controls how strongly a parser participates in ref-ownership matching:

- `0` is the normal default strength
- higher values win over lower values when multiple apps match
- negative values only participate for still-untracked / cluster-only resources at that stage

For app-defined parser groups under `global.hydra.refs`, prefer placing `priority` on the group next to `tag`. That group-level value becomes the default for all contained `ref-parsers`, and an individual parser can still override it when needed.

## Pick Rules

Each `pick` rule is a CEL expression that returns a list of `RefDefinition` objects using the `refBuilder()` API:

```yaml
pick:
  - cel: |
      [refBuilder().outgoing(id(gvk, ns, name))]
```

### refBuilder() API

```
refBuilder()                    → Start building a ref
  .incoming(endpoint)           → Create ref pointing TO this resource
  .outgoing(endpoint)           → Create ref pointing FROM this resource
  .label(string)                → Set the ref label
  .tag(string)                  → Add a tag
  .desc(string)                 → Set description
  .attribute(key, value)        → Add an attribute
```

### Endpoint Functions

```
id(gvk, ns, name)             → Reference by resource ID
idString("gvk", "ns", "name") → Reference by string literals
ref(provider, value)           → Non-ID reference (external)
```

## Example: Self-Reference

Every resource gets an incoming ref to itself (enables graph connectivity):

```yaml
ref-parsers:
  - predicate: "true"
    pick:
      - cel: '[refBuilder().incoming(id(gvk, ns, name))]'
```

## Example: Owner References

Extract dependencies from Kubernetes `ownerReferences`:

```yaml
ref-parsers:
  - predicate: 'has(entity.metadata.ownerReferences) && entity.metadata.ownerReferences.size() > 0'
    reverse: true
    label: owner
    pick:
      - cel: |
          entity.metadata.ownerReferences.filter(o, has(o.controller) && o.controller).map(o,
            refBuilder().outgoing(id(o.apiVersion + "/" + o.kind, ns, o.name))
              .attribute("kubernetes:ownerController", "true")
          )
```

## Example: Namespace Dependency

Every namespaced resource depends on its Namespace:

```yaml
ref-parsers:
  - predicate: 'namespaced && ns != ""'
    label: namespace
    pick:
      - cel: '[refBuilder().outgoing(id("v1/Namespace", "", ns))]'
```

## The `reverse` Flag

When `reverse: true`, the edge direction is stored inverted. This is used for `ownerReferences` where the child stores the ref but the logical direction is parent → child.

## See Also

- [Writing Custom Refs](writing-custom-refs.md) — Create your own parsers
- [CEL: Functions](../cel/functions.md) — refBuilder() and id() reference
- [CEL: Variables](../cel/variables.md) — Available variables in predicates
