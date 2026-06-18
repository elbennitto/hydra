# Ref Labels

Labels categorize refs by their semantic meaning.

## Available Labels

### `namespace`

The resource depends on its Namespace. Nearly every namespaced resource has this ref.

```
apps/v1/Deployment/kube-system/coredns
  ──[namespace]──▶ v1/Namespace//kube-system
```

### `crd`

The resource depends on a CustomResourceDefinition. Custom resources cannot exist without their CRD.

```
cert-manager.io/v1/Certificate/default/my-cert
  ──[crd]──▶ apiextensions.k8s.io/v1/CustomResourceDefinition//certificates.cert-manager.io
```

### `controller`

The resource is managed by a controller (via ownerReferences with `controller: true`).

```
apps/v1/ReplicaSet/default/myapp-abc123
  ──[controller]──▶ apps/v1/Deployment/default/myapp
```

### `owner`

The resource has an ownerReference to another resource (non-controller ownership).

```
v1/Pod/default/myapp-abc123-xyz
  ──[owner]──▶ apps/v1/ReplicaSet/default/myapp-abc123
```

### `workloadRegardingEvent`

An Event that relates to a workload. Used to associate Events with the resources they describe.

```
events.k8s.io/v1/Event/default/myapp.17abc
  ──[workloadRegardingEvent]──▶ apps/v1/Deployment/default/myapp
```

## Custom Labels

Ref-parsers and value-defined refs can set custom labels via:

```yaml
# In ref-parser
label: my-custom-label

# In refBuilder CEL
refBuilder().outgoing(id(...)).label("my-label")
```

## Owner Resolution `via` Values

When Hydra prints preset owner-resolution paths, each hop can include a `via:` marker showing how
the parent candidate was reached.

Possible values are:

### `owner-ref`

The parent came from Kubernetes `metadata.ownerReferences`.

### `regarding-ref`

The parent came from an Event's direct `regarding` / `involvedObject` subject reference.

### `workload-regarding-event-ref`

The parent came from a synthetic workload-to-Event anchor ref labeled `workloadRegardingEvent`.
This is used when Hydra can connect an Event back to a workload even if the intermediate subject is
not available as a full live object.

### `objectset-owner`

The parent came from Rancher wrangler `objectset.rio.cattle.io/owner-*` annotations.

### `podMetrics`

The parent came from a ref labeled `podMetrics`, typically linking a workload or Pod to
`metrics.k8s.io/v1beta1/PodMetrics`.

## Labels in the TUI

Labels are displayed in the interactive TUI (`hydra local inspect`, `hydra gitops inspect`) to help identify the nature of each edge at a glance.

## See Also

- [Ref Parsers](ref-parsers.md) — Setting labels in parsers
- [Ref Attributes](ref-attributes.md) — Finer-grained metadata
