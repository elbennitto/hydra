# hydra gitops untracked

List live cluster resource ids that remain **unassigned** after Hydra builds the shared resource model.

## Synopsis

```text
hydra gitops untracked <cluster> [flags]
```

## Description

`hydra gitops untracked` is **read-only**. For the given **cluster** name (a single path segment, **no** `.` — same convention as other `hydra gitops` commands), Hydra:

1. Resolves all applications on that cluster after `--exclude-app`.
2. Renders the selected apps per app, normalizes template API versions, and lists the full live inventory.
3. Builds the shared `BuildResourceModel` from template and cluster entities.
4. Assigns resources through direct template ownership, preset apps, owner references, generic workload refs, and Event `regarding` / `related` refs.
5. Keeps only live **ownership roots** that remain unassigned after the shared model is complete.

The printed output is one Hydra resource id per line for objects that are still unexplained after model assignment.

Optional **`--include`** / **`--exclude`** CEL filters narrow the printed set (same mechanism as [`hydra gitops list`](list.md)).

## Flags

Inherited cluster flags include **`--hydra-context`**, **`--exclude-app`**, **`--helm-network-mode`**, **`--no-cache`**, **`--parallel`**, **`--qps`**, **`--api-burst`**, **`--include`**, **`--exclude`**, and the usual REST client options. See `hydra gitops untracked --help`.

## Examples

```bash
hydra gitops untracked prod

hydra gitops untracked prod --qps=60 --parallel=8
```

## Related

- [`hydra gitops system`](system.md) — preset apps and live matches
- [`hydra gitops uninstall`](uninstall.md) — cleanup planning based on the same resource model
