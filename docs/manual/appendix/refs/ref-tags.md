# Ref Tags

Tags are behavioral markers on refs that control how Hydra treats resources during operations.

## Available Tags

### `backup`

Resources reachable via refs tagged `[backup]` are included when running `hydra gitops backup create`.

```yaml
refs:
  ca-secret:
    tag: [backup]
    predicate: 'kind == "Secret" && name == "ca-key-pair"'
```

The `backup` tag also participates in uninstall ownership selection, because backup material must be removed with the app that owns it.

### `uninstall`

Resources reachable via refs tagged `[uninstall]` are removed during `hydra gitops uninstall`.
For cluster-only resources that are not present in a standalone app render, `[uninstall]`
also participates in app ownership assignment: if exactly one app's uninstall predicates match,
Hydra assigns the resource to that app so it can be planned and removed with the app.

```yaml
refs:
  events:
    tag: [uninstall]
    predicate: 'gvk == "events.k8s.io/v1/Event" && ns == "my-namespace"'
```

### `uninstall-force`

Marks resources that belong to the app but should not be deleted by a normal uninstall without an
explicit operator decision, for example PVCs or other important runtime-created resources.
During a normal `hydra gitops uninstall`, Hydra shows these resources in the planned deletion
summary. Re-run with `--force` to delete them, or with `--keep` to skip deleting them and continue.

### `uninstall-safe`

Marks resources as safe to remove. Used for informational purposes and less strict validation during uninstall planning.

### `optional:startup`

The dependency is optional for startup ordering. During scale/apply startup planning, refs with this tag are delayed behind required refs rather than blocking the required startup path.

### `optional:ref`

The ref comes from a Kubernetes field marked optional, such as an optional ConfigMap or Secret reference in a Pod spec. Hydra records the edge for graph visibility while preserving that Kubernetes optionality.

### `bootstrap-guard`

The target **must** exist before Hydra allows a non-bootstrap apply. This is the guard mechanism that prevents applying when critical infrastructure is missing.

```bash
# Skips bootstrap-guard validation
hydra gitops apply 'prod.**' --bootstrap

# Also skips it
hydra gitops apply 'prod.**' --skip-bootstrap-guard
```

### `runtime`

The ref or ref-ownership rule exists only at runtime and cannot be resolved from templates. For ownership checks, Hydra evaluates `runtime` rules only for cluster-only resources that have no standalone template render.

## Multiple Tags

A ref can have multiple tags:

```yaml
refs:
  important-secret:
    tag: [backup, uninstall]
    predicate: 'kind == "Secret" && name == "critical-data"'
```

## Tag Application

Tags can be set:
- In `global.hydra.refs` values (see [Values: refs](../values/refs-in-values.md))
- In ref-parser definitions (applies to all refs produced by that parser)
- In the `refBuilder().tag("...")` CEL API

## See Also

- [Values: refs](../values/refs-in-values.md)
- [Ref Parsers](ref-parsers.md)
- [Concepts: Bootstrap](../concepts/bootstrap.md) — bootstrap-guard behavior
