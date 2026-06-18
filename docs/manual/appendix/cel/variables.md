# CEL Variables

All variables available in CEL expressions, organized by context.

## Entity Variables (Always Available)

These are available in all CEL contexts (ref-parsers, presets, CLI filters, value predicates):

| Variable | Type | Description |
|----------|------|-------------|
| `gvk` | `string` | Full GVK: `"apps/v1/Deployment"` |
| `gvkn` | `string` | GVK + namespaced name |
| `id` | `string` | Full resource ID: `"apps/v1/Deployment/default/myapp"` |
| `kind` | `string` | Kind only: `"Deployment"` |
| `group` | `string` | API group: `"apps"` |
| `apiVersion` | `string` | API version: `"apps/v1"` |
| `resource` | `string` | Resource type (plural) |
| `name` | `string` | Resource name |
| `ns` | `string` | Namespace (empty for cluster-scoped) |
| `namespaced` | `bool` | `true` if resource is namespaced |

## App Context Variables

Available when evaluating in the context of a specific app:

| Variable | Type | Description |
|----------|------|-------------|
| `appId` | `string` | Full app ID: `"prod.cluster-infra.cert-manager"` |
| `appNamespace` | `string` | App's primary namespace |
| `templatePath` | `string` | Path to the template file |
| `appOwned` | `bool` | `true` if resource belongs to current app |
| `builtIn` | `bool` | `true` if resource is a cluster builtin |
| `selected` | `bool` | `true` if resource is in the current selection |

## Object Variables (Ref-Parsers Only)

Full resource objects available in ref-parser pick rules:

| Variable | Type | Description |
|----------|------|-------------|
| `entity` | `map` | Full resource object (rendered template) |
| `clusterEntity` | `map` | Live cluster version of the resource |
| `templateEntity` | `map` | Template-rendered version |
| `dryRunEntity` | `map` | Dry-run applied version |
| `leftEntity` | `map` | Left side in diff context |
| `rightEntity` | `map` | Right side in diff context |

### Accessing Object Fields

```cel
entity.metadata.name
entity.metadata.namespace
entity.metadata.annotations["key"]
entity.metadata.ownerReferences
entity.spec.replicas
entity.status.readyReplicas
```

Always use `has()` for optional fields:

```cel
has(entity.metadata.annotations) && entity.metadata.annotations["key"] == "value"
has(entity.spec.selector) && has(entity.spec.selector.matchLabels)
```

## Cluster Inventory Variables (Cluster Context Only)

Available when cluster state is loaded (ref-parsers with cluster overlay):

| Function | Return Type | Description |
|----------|-------------|-------------|
| `clusterEntities()` | `[]map` | All entities on the cluster |
| `clusterEntities(selector)` | `[]map` | Cluster entities matching a selector object |
| `managedNamespaces()` | `[]map` | All managed namespaces |
| `templateEntities()` | `[]map` | All template-rendered entities |
| `templateEntities(selector)` | `[]map` | Template entities matching a selector object |
| `involvedObjectEvents(id)` | `[]map` | Events for a workload |

## Context Availability Matrix

| Variable | CLI Filter | Ref-Parser Predicate | Ref-Parser Pick | Preset CEL | Value Predicate |
|----------|-----------|---------------------|-----------------|------------|-----------------|
| `gvk`, `kind`, `name`, `ns` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `appOwned`, `builtIn` | ✓ | ✓ | ✓ | | ✓ |
| `entity` (object) | | ✓ | ✓ | | |
| `clusterEntities()` | | | ✓ (cluster) | | |

## See Also

- [CEL: Functions](functions.md)
- [CEL: Predicates](predicates.md)
