# hydra gitops system

Show merged **`global.hydra.presets`** cluster-defaults configuration, which live cluster resources match each builtin CEL line, and whether each explicit **`ids`** entry is present in the live inventory.

## Synopsis

```text
hydra gitops system <cluster> [flags]
```

## Description

`hydra gitops system` is a **read-only** diagnostic. It does not modify the cluster.

**Stdout** is human-readable aligned **text** by default. With **`--yaml`**, Hydra prints one structured **YAML** document instead (optional **syntax highlighting** when color is enabled via **`--color`**, **`--no-color`**, or **`--color-mode`** and a TTY).

For the given **cluster** name (a single path segment, **no** `.` — same convention as [`hydra gitops review cluster`](review.md)), Hydra resolves all applications defined for that cluster (after `--exclude-app`), renders **every** app on the cluster for the same inventory used elsewhere (`cluster.AppIds` + `RenderClusterSelectedApps`), normalizes template API versions, merges **`global.hydra.presets`** from Helm values and Hydra ConfigMap documents in deterministic app order (same merge as [`hydra gitops uninstall`](uninstall.md) and ref ownership review), lists the full live inventory, and then reads both template and live sides from the shared resource model before printing a structured report:

- **`cluster`**: cluster name.
- **`matchCount`** / **`missingCount`**: aggregated across all presets (explicit **`ids`** matches plus CEL **`matchCount`** sums per predicate; **`missingCount`** counts explicit **`ids`** not present in the live inventory after kubernetes minor gating; CEL lines do not contribute to **`missingCount`**).
- **`missingIds`**: sorted union of explicit **`ids`** that are missing at the top level (same gating as per-id **`missingCount`**).
- **`presets`**: for each **effectively enabled** top-level preset (unless **`--all`**, see flags), e.g. **`coredns`**, **`kubernetes`**, **`flannel`**, …: **`builtinDefaultEnabled`**, **`effectiveEnabled`**, per-preset **`matchCount`** / **`missingCount`** / **`missingIds`** (aggregates over that preset’s predicates), and **`predicates`**.
- Each predicate: **`name`**, **`enabled`**, predicate-level **`matchCount`** / **`missingCount`** / **`missingIds`** ( **`missingIds`** lists only explicit **`ids`** in that predicate that are missing), optional **`ids`** (each with **`matchCount`** and **`missingCount`** `0` or `1` after kubernetes minor gating), and **`celLines`**.
- Each CEL line: **`index`**, **`expression`**, **`matchCount`**, and **`matchIds`** (Hydra resource ids, lexicographically sorted within that line; same CEL environment as uninstall/review: entity variables include **`gvk`**, **`ns`**, **`name`**, with **`cel.NewEnvWithEntityInventory`** over the full-cluster template render).
- Within each predicate, explicit **`ids`** entries are ordered **lexicographically by id** in both YAML and text output.

In the default **text** output (without **`--yaml`**), rows are **deduplicated by resource id**: the same id can appear once from explicit **`ids`** and again from a synthesized rbac CEL line (for example `id == "…"` with no live match). **Found** beats **not found** when merging. The resulting rows are sorted lexicographically by id.

The command itself is read-only, but it now reads template and live inventories from the same per-ID records that also feed `hydra gitops untracked` and parts of `hydra gitops uninstall`.

On very large clusters the command may be slow because it evaluates every CEL line against the full snapshot.

During **`ListClusterAll`**, Hydra can show the same **footer discovery progress** as [`hydra gitops apply`](apply.md): enabled when stderr is a TTY **and** colors are on (see global **`--no-color-log`** / **`--color-log`**). Disable the bar with the global **`--no-progress`** flag (logs still appear on stderr).

After the inventory listing, a second footer bar **`cluster system · report`** advances in **1 + N** steps: read the Kubernetes server minor version, then one step per **CEL line** in the presets that are included in the report (**`N`** counts all merged presets when **`--all`** is set; otherwise only **effectively enabled** presets). Each step shows a truncated preset · predicate · expression detail. Without a TTY progress UI, the same steps are logged at **INFO** as `cluster system post-list`. **`--no-progress`** uses the dummy footer (no mpb bar) but can still emit step detail at debug level, like other cluster commands.

## Flags

| Flag | Description |
| --- | --- |
| `--hydra-context` | Path to the Hydra context directory (or `HYDRA_CONTEXT`) |
| `--exclude-app` | Glob pattern to exclude apps from the merge/render set (repeatable) |
| `--helm-network-mode` | `online`, `local`, `offline`, or `error` (Helm chart resolution) |
| `--no-cache` | Disable persistent Helm template cache for this run |
| `--parallel` | Number of concurrent workers for listing live cluster API resources (`0` = [GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS), capped at `64`; default `0`; footer shows one status line per worker when the effective value is `>1`, same as apply’s discovery listing) |
| `--all` | Also list **effectively disabled** presets (and run their CEL evaluations for the report and progress bar). Without **`--all`**, the report and **`matchCount`** / **`missingCount`** aggregates include **enabled** presets only — the same scope **`hydra gitops system`** used before this flag existed. |
| `--yaml` | Emit the report as YAML instead of aligned text |
| `--color`, `-c` | Force colored output |
| `--no-color` | Plain output even in a terminal |
| `--color-mode` | `auto` (default), `always`, or `never` |
| REST client flags | Same as other `hydra gitops` commands (`--qps`, `--api-burst`) |

Context and kubeconfig flags follow the same rules as [`hydra gitops list`](list.md).

## Examples

```bash
# Inspect presets for cluster "prod" in the current Hydra context
hydra gitops system prod

# Same with an explicit context path
hydra gitops system prod --hydra-context /path/to/hydra/context

# Include disabled presets (e.g. canal when flannel is active) in text or YAML output
hydra gitops system prod --all
```

## Related

- [`hydra gitops untracked`](untracked.md) — live ids not covered by templates, presets, ownerReferences, or `priority >= 0` uninstall ownership refs
- [`hydra gitops review cluster`](review.md) — ref ownership findings involving presets
- [`hydra gitops uninstall`](uninstall.md) — warn-listed leftovers filtered by presets
- Architecture: [uninstall selection — global.hydra.presets](../../../develop/hydra-go/details/commands/deletion-and-topology/uninstall-selection.md#global-hydra-presets--builtin-cel-cluster-defaults)
