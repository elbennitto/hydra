# Ref Types

Every ref has a type that describes the nature of the dependency.

## direct

An explicit, definite dependency. The source resource **requires** the target to function.

Examples:
- Deployment → ServiceAccount (can't run without it)
- Deployment → Namespace (can't exist outside it)
- Certificate → ClusterIssuer (can't be issued without it)

Direct refs are the most common type and drive topological ordering.

## indirect

A transitive or implied dependency. The source does not directly reference the target, but the target is necessary through an intermediate relationship.

Examples:
- A Deployment indirectly depends on a CRD if it uses a custom resource that depends on that CRD
- A Service indirectly depends on the Namespace of the Pods it selects

Indirect refs contribute to ordering but may be more lenient in validation.

## runtime

A relationship that exists only at runtime and is not visible in the static manifest. The dependency cannot be resolved from templates alone — it requires cluster state.

Examples:
- A Pod → the Node it is scheduled on
- A PersistentVolumeClaim → the PersistentVolume bound to it

Runtime refs are discovered through cluster observation, not template analysis.

## regarding

An informational association, typically between Events and the resources they describe. These do not affect ordering or operations but improve visibility.

Examples:
- Event → Deployment (the event reports about the deployment)
- Event → Pod (the event describes a pod lifecycle change)

Regarding refs are shown in the TUI for context but do not influence apply/uninstall behavior.

## Summary

| Type | Affects Ordering | Requires Resolution | Typical Source |
|------|-----------------|--------------------|--------------| 
| direct | Yes | Yes | Template analysis |
| indirect | Yes | Lenient | Transitive resolution |
| runtime | No | Cluster state | Live observation |
| regarding | No | No | Event correlation |

## See Also

- [Ref Parsers](ref-parsers.md) — How types are assigned during parsing
- [Concepts: Dependency Graph](../concepts/dependency-graph.md)
