# CEL Examples

Practical examples for common Hydra CEL use cases.

## CLI Filtering

### Show only Deployments that are out of sync

```bash
hydra gitops diff prod.** --include 'kind == "Deployment"'
```

### Exclude Events and PodMetrics from diff

```bash
hydra gitops diff prod.** --exclude 'kind == "Event" || gvk.startsWith("metrics.k8s.io/")'
```

### Only show resources in a specific namespace

```bash
hydra gitops diff prod.cluster-infra.* --include 'ns == "ingress-nginx"'
```

### Find all Secrets across apps

```bash
hydra local find 'prod.**' --include 'kind == "Secret"' --pick 'name' --uniq
```

## Ref-Parser Predicates

### Match all Deployments

```cel
gvk == "apps/v1/Deployment"
```

### Match resources with specific annotations

```cel
has(entity.metadata.annotations) && has(entity.metadata.annotations["app.kubernetes.io/managed-by"])
```

### Match resources with ownerReferences

```cel
has(entity.metadata.ownerReferences) && entity.metadata.ownerReferences.size() > 0
```

### Match CRDs of a specific group

```cel
gvk == "apiextensions.k8s.io/v1/CustomResourceDefinition" && name.matches(".*\\.cert-manager\\.io")
```

## Ref-Parser Pick Rules

### Simple namespace dependency

```cel
[refBuilder().outgoing(id("v1/Namespace", "", ns))]
```

### Extract all ownerReferences as refs

```cel
entity.metadata.ownerReferences.map(o,
  refBuilder().outgoing(id(o.apiVersion + "/" + o.kind, ns, o.name))
    .label("owner")
)
```

### Conditional extraction (return empty list if field missing)

```cel
has(entity.spec.secretName) ?
  [refBuilder().outgoing(id("v1/Secret", ns, entity.spec.secretName)).label("secret")] :
  []
```

### Extract from a list field

```cel
has(entity.spec.volumes) ?
  entity.spec.volumes.filter(v, has(v.configMap)).map(v,
    refBuilder().outgoing(id("v1/ConfigMap", ns, v.configMap.name))
      .label("volume-configmap")
  ) : []
```

### Associate Events with their involved objects

```cel
clusterEntities({"namespace": ns, "gvk": "events.k8s.io/v1/Event"}).filter(e,
  e.name.startsWith(name)
).map(e,
  refBuilder().outgoing(id("events.k8s.io/v1/Event", ns, e.name))
    .label("workloadRegardingEvent")
)
```

## Preset CEL Patterns

### Match by name pattern

```cel
gvk == "v1/Pod" && ns == "kube-system" && name.matches("^coredns-[a-z0-9]+-[a-z0-9]+$")
```

### Match DaemonSet pods

```cel
gvk == "v1/Pod" && ns == "kube-system" && name.startsWith("kube-proxy-")
```

### Match PodMetrics (optional)

```cel
gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "kube-system" && name.matches("^coredns-.*")
```

## Value Predicates

### TemplatePatch: Add annotation to all Applications

```cel
kind == "Application" && gvk.startsWith("argoproj.io/")
```

### Diff ignore: Skip all Events

```cel
gvk == "events.k8s.io/v1/Event"
```

### Clone predicate: Select specific Secret

```cel
kind == "Secret" && ns == "kyverno" && name == "registry-credentials"
```

### Ready probe: Check Deployment availability

```cel
kind == "Deployment" && has(entity.status.availableReplicas) && entity.status.availableReplicas >= entity.spec.replicas
```

## Tips

- Always use `has()` before accessing optional fields
- Return `[]` (empty list) from pick rules when there's nothing to extract
- Use ternary `condition ? value_if_true : value_if_false` for conditional logic
- Chain `.filter().map()` for extracting refs from collections
- String methods: `.startsWith()`, `.matches()`, `.contains()`

## See Also

- [CEL: Variables](variables.md)
- [CEL: Functions](functions.md)
- [CEL: Predicates](predicates.md)
