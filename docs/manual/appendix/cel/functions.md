# CEL Functions

All functions available in Hydra's CEL environment.

## Ref Builder

The ref builder is a chainable API for constructing dependency edges in ref-parser pick rules.

### refBuilder()

Creates a new empty ref builder.

```cel
refBuilder()
```

### .incoming(endpoint)

Creates a ref pointing **to** the current resource from the endpoint.

```cel
refBuilder().incoming(id("v1/Service", ns, name))
```

### .outgoing(endpoint)

Creates a ref pointing **from** the current resource to the endpoint.

```cel
refBuilder().outgoing(id("v1/Namespace", "", ns))
```

### .label(string)

Sets the semantic label for the ref.

```cel
refBuilder().outgoing(id("v1/Namespace", "", ns)).label("namespace")
```

### .tag(string)

Adds a behavioral tag to the ref.

```cel
refBuilder().outgoing(id("v1/Secret", ns, name)).tag("backup")
```

### .desc(string)

Adds a description to the ref.

```cel
refBuilder().outgoing(id(...)).desc("TLS certificate dependency")
```

### .attribute(key, value)

Adds a metadata attribute.

```cel
refBuilder().outgoing(id(...)).attribute("origin", "app")
```

## Endpoint Functions

### id(gvk, ns, name)

Creates a resource endpoint from GVK, namespace, and name variables:

```cel
id(gvk, ns, name)                    // Current resource's identity
id("v1/Namespace", "", ns)           // Namespace resource
id("v1/ServiceAccount", ns, name)    // ServiceAccount in same ns
id(o.apiVersion + "/" + o.kind, ns, o.name)  // From ownerReference
```

### idString(gvk, ns, name)

Like `id()` but all arguments are treated as string literals:

```cel
idString("apps/v1/Deployment", "kube-system", "coredns")
```

### ref(provider, value)

Creates a non-ID reference endpoint (for external/abstract references):

```cel
ref("helm-chart", "ingress-nginx")
```

## Cluster Inventory Functions

Available only when cluster state is loaded.

### clusterEntities()

Returns all entities from the cluster inventory:

```cel
clusterEntities().filter(e, e.gvk == "v1/Pod" && e.ns == "default")
```

### clusterEntities(selector)

Returns entities prefiltered by a selector object. Supported fields follow Hydra's resource
selector shape, for example `group`, `version`, `kind`, `apiVersion`, `gvk`, `namespace`/`ns`,
`gvkn`, `name`, and `id`.

```cel
clusterEntities({"namespace": ns}).filter(e, e.gvk == "events.k8s.io/v1/Event")
clusterEntities({"namespace": ns, "gvk": "events.k8s.io/v1/Event"})
```

### managedNamespaces()

Returns all namespaces that are managed by Hydra apps.

### templateEntities()

Returns all entities from rendered templates.

### templateEntities(selector)

Returns template entities prefiltered by a selector object:

```cel
templateEntities({"namespace": ns, "gvk": "v1/ConfigMap"})
```

### involvedObjectEvents(workloadId)

Returns Events related to a specific workload:

```cel
involvedObjectEvents(id(gvk, ns, name))
```

## Utility Functions

### has(field)

Null-safe field existence check:

```cel
has(entity.metadata.annotations)
has(entity.spec.selector.matchLabels)
```

### string(value)

Convert a value to string:

```cel
string(entity.spec.replicas)
```

### annotations(entity) / labels(entity)

Extract annotations or labels as `map[string]string`:

```cel
annotations(entity)["key"]
labels(entity)["app.kubernetes.io/name"]
```

### ordinalRange(start, count)

Generate a sequence of integers:

```cel
ordinalRange(0, 3)  // [0, 1, 2]
```

## List Operations

Standard CEL list methods:

```cel
list.map(item, expr)           // Transform each element
list.filter(item, condition)   // Keep matching elements
list.size()                    // Count elements
string.startsWith(prefix)     // String prefix check
string.matches(regex)         // Regex match
list.contains(elem)           // Element membership
```

### Example: Map ownerReferences to Refs

```cel
entity.metadata.ownerReferences.filter(o, has(o.controller) && o.controller).map(o,
  refBuilder().outgoing(id(o.apiVersion + "/" + o.kind, ns, o.name))
    .label("owner")
    .attribute("kubernetes:ownerController", "true")
)
```

## See Also

- [CEL: Variables](variables.md)
- [CEL: Examples](examples.md)
- [Refs: Ref Parsers](../refs/ref-parsers.md)
