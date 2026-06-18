<!-- markdownlint-disable MD013 -->

# hydra gitops show

Print the central Hydra app-assignment view for live cluster resources.

## Synopsis

```text
hydra gitops show <cluster> [flags]
```

## Description

`hydra gitops show` is a **read-only** ownership audit. It does not render one app in isolation and it does not diff desired vs live state in the usual sense. Instead, it builds the shared resource model and asks one question:

Which live Kubernetes resources can Hydra assign to exactly one app on this cluster?

For the given **cluster** name (a single path segment, **no** `.`), Hydra:

1. Resolves the cluster and the enabled app IDs after `--exclude-app`.
2. Renders the selected apps per app, normalizes template API versions, and lists the live cluster inventory.
3. Builds the shared `BuildResourceModel` from template and cluster entities.
4. Materializes cluster-default presets as preset apps such as `in-cluster.preset.coredns`.
5. Reads the resulting assignment view and prints:
   - a compact table by default
   - full YAML with `--yaml`
   - or YAML blocks containing only `ambiguous` / `unassigned` sections when assignment is not clean

The ownership model uses the same central logic as other cluster-aware workflows. The strongest assignment signals are:

1. template ID match
2. preset app anchors
3. `metadata.ownerReferences`
4. ref-ownership predicates with `priority >= 0` from ref groups tagged `uninstall`, `uninstall-force`, or `backup`
5. ref-ownership predicates with `priority < 0`, including groups tagged only `uninstall-safe`

Both ref-ownership priority passes evaluate only live resources that remain unassigned. Hydra checks owner-reference roots before children; once a root is assigned to one app, single-owner children inherit that app immediately and are skipped by later predicate checks.

If a live object can be assigned to more than one app, Hydra reports it as **ambiguous**. If it cannot be assigned to any app, Hydra reports it as **unassigned**. In both cases the command exits non-zero.

This makes `show` a useful audit tool before relying on `untracked` or uninstall-related ownership decisions.

For the deeper background on how the shared inventory is built, see [Concepts: Cluster Command Data Model](../../appendix/concepts/cluster-command-data-model.md).

## Output

Default output is a short table with:

- app ID
- assigned live resource count

With `--yaml`, Hydra prints a structured document containing:

- `cluster`
- `apps`
- `ambiguous`
- `unassigned`

Ambiguous and unassigned resources include assignment reasons when available, so you can see whether the collision came from preset apps, owner references, or ref-ownership rules.

## Flags

| Flag | Description |
| --- | --- |
| `--hydra-context` | Path to the Hydra context directory (or `HYDRA_CONTEXT`) |
| `--exclude-app` | Glob pattern to exclude apps from the ownership universe (repeatable) |
| `--helm-network-mode` | `online`, `local`, `offline`, or `error` |
| `--no-cache` | Disable persistent Helm template cache for this run |
| `--parallel` | Concurrent workers for full live-cluster listing (`0` = `GOMAXPROCS`, capped) |
| `--builtin` | Include preset apps such as `in-cluster.preset.coredns` in the output |
| `--yaml` | Emit the full YAML report instead of the default table |
| `--color`, `--no-color`, `--color-mode` | Control colored YAML / table output |
| REST client flags | Same as other `hydra gitops` commands (`--qps`, `--api-burst`) |

## Examples

```bash
hydra gitops show prod

hydra gitops show prod --yaml

hydra gitops show prod --builtin --yaml
```

## Related

- [Concepts: Cluster Command Data Model](../../appendix/concepts/cluster-command-data-model.md)
- [hydra gitops untracked](untracked.md)
- [hydra gitops system](system.md)
- [hydra gitops uninstall](uninstall.md)
