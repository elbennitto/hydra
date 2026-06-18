<!-- markdownlint-disable MD013 -->

# Cluster Command Data Model

Hydra uses one shared resource model for local, cluster, and GitOps commands.

The model has one row per Hydra resource id. A row can contain a rendered template object, a live cluster object, or both. Ownership is also stored on the same row, so commands do not need separate preset, app-assignment, or untracked pipelines.

## One Builder

All commands build the model through `BuildResourceModel`.

- Local commands pass template entities and leave cluster entities unset.
- Cluster commands pass cluster entities and leave template entities unset.
- GitOps commands pass both.
- Passing neither side is an error.
- An empty entity list is valid and means “this side was intentionally loaded and contains no objects”; `nil` means “this side was not part of the command”.

The builder returns the same row shape in every mode:

- `id`
- `template`
- `cluster`
- `assignedApp`
- `assignmentReasons`
- `ambiguousApps`
- `unassigned`

## Presets Are Apps

Cluster-default presets are represented as normal app assignments.

For example, the CoreDNS preset is assigned as:

```text
in-cluster.preset.coredns
```

There is no separate preset ownership layer. A caller can check whether an app is a preset app through the app id type, for example `AppId.IsPresetApp()`.

Preset anchors and preset CEL matches assign resources to the matching preset app. Those resources then participate in the same ownership propagation as any other app-owned resource.

## Ownership Pipeline

The builder applies ownership in one central order:

1. Per-app rendered resources are assigned directly to the app that rendered them.
2. Rendering the same resource from more than one app is a model error; Hydra collects all duplicate ids and reports them together.
3. Preset anchors are materialized as preset-app template resources.
4. Kubernetes owner references and generic refs propagate ownership from parents to children.
5. Cluster-only resources that still have no owner are matched against `priority >= 0` teardown ref ownership rules from `uninstall`, `uninstall-force`, or `backup` ref groups.
6. Any resources still unassigned after that are matched against `priority < 0` teardown ref ownership rules, including `uninstall-safe`-only ref groups.
7. Remaining resources become either ambiguous or unassigned.

Template-only parents are valid ownership anchors. If a Deployment was rendered by Hydra but has already been deleted from the cluster, a remaining live ReplicaSet, Pod, PodMetrics, or Event can still be assigned to the Deployment’s app through generic workload refs.

During the teardown ref passes, Hydra evaluates only resources that are still unassigned. It orders the pending live resources so owner-reference roots are checked before their children. When a root resource is assigned to exactly one app, children that have that root as their single live owner are assigned to the same app immediately and are skipped by later `priority >= 0` or `priority < 0` predicate checks. The same root-first ordering is used by the soft owner-reference expansion path used by review-style audits. Before compiling CEL ownership predicates, Hydra also skips per-app CEL inventory setup for apps that have no ownership predicates in the current run.

## Generic Workload And Event Refs

The model includes generic Kubernetes workload relationships, including:

- `Deployment -> ReplicaSet`
- `Deployment -> Pod`
- `Deployment -> Event`
- `ReplicaSet -> Pod`
- `ReplicaSet -> Event`
- `Pod -> Event`

Event `regarding`, `related`, and older `involvedObject` references are matched against both live cluster entities and template entities. This is what lets Events remain attributable even when the live parent object has disappeared.

## Command Behavior

Commands read from the completed model instead of rebuilding ownership logic.

| Command | Model Input | Main Use |
| --- | --- | --- |
| `hydra local ...` | templates only | Rendered desired-state inspection |
| `hydra gitops list`, `dump`, `status` | cluster only | Live inventory inspection |
| `hydra gitops show` | templates + cluster | App assignment audit |
| `hydra gitops untracked` | templates + cluster | Unassigned live ownership roots |
| `hydra gitops system` | templates + cluster | Preset-app diagnostics |
| `hydra gitops uninstall` | templates + cluster | Ownership-aware cleanup planning |

`untracked` is intentionally simple: it prints live ownership roots that remain unassigned after the shared model has applied templates, preset apps, owner refs, and generic refs.

## Related

- [Dependency Graph](dependency-graph.md)
- [GitOps Workflow](gitops-workflow.md)
- [hydra gitops show](../commands/cluster/show.md)
- [hydra gitops untracked](../commands/cluster/untracked.md)
