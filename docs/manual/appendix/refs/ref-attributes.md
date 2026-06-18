# Ref Attributes

Attributes provide fine-grained metadata about where a ref came from and how it was created.

## Origin Attributes

### `origin:app`

The ref was defined in the app's `global.hydra.refs` values. It comes from explicit configuration by the chart author.

### `origin:owner`

The ref was extracted from Kubernetes `ownerReferences` in the resource metadata.

### `origin:objectset`

The ref was extracted from a Rancher/Fleet ObjectSet relationship.

### `origin:generated`

The ref target was generated through virtual materialization — a resource that does not exist in templates but was inferred from ref targets.

## Kubernetes Attributes

### `kubernetes:ownerController`

Set to `"true"` when the ownerReference had `controller: true`. Distinguishes controller ownership from non-controller ownership.

## Setting Attributes

In ref-parsers:

```yaml
pick:
  - cel: |
      [refBuilder().outgoing(id("v1/Secret", ns, name))
        .attribute("origin", "app")]
```

In the refBuilder API:

```
refBuilder().attribute(key, value) → CelRef
```

## Usage

Attributes are used internally for:
- Debugging ref origins (where did this edge come from?)
- Filtering in review modes
- Distinguishing between manually defined and automatically discovered refs

They are visible in the interactive TUI and in `hydra local review` / `hydra gitops review` output.

## See Also

- [Ref Parsers](ref-parsers.md)
- [Ref Labels](ref-labels.md)
