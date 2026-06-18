# hydra inspect (shared)

Interactive TUI for navigating the reference graph of a Hydra resource.

Both [`hydra local inspect`](local/inspect.md) and
[`hydra gitops inspect`](cluster/inspect.md) share the same
interactive interface described on this page. The **cluster** variant combines
locally rendered templates with a live cluster connection for ref resolution.
For that variant, the starting `<id>` is also accepted when it exists only on
the live cluster (for example operator-created workloads), not only when it
appears in templates. The **local** variant always renders templates offline.

## Synopsis

```text
hydra local inspect   <cluster> [id] [flags]
hydra gitops inspect <cluster> [id] [flags]
```

## Arguments

| Argument | Description |
| -------- | ----------- |
| `cluster` | Cluster name (same segment as in app ids; must not contain `.`). |
| `id` | Optional. Canonical Hydra resource id: `group/version/kind/namespace/name`, or for core API groups `version/kind/namespace/name`. If omitted, Hydra opens an **id picker** first (see below). |

## ID picker

When you pass only `<cluster>`, Hydra shows every known resource id (templates only for
`hydra local inspect`; for `hydra gitops inspect`, templates plus live objects).
The picker starts on the list. A **help/hotkey line** is displayed between the picker header and the list (not as a footer). Press **`/`** to open the filter popup. Inside the popup:

- The **search input** is focused by default.
- **Tab** moves focus to the **field dropdown**; **Shift+Tab** moves back.
- Type a case-insensitive substring query in the input.
- Choose which field to match in the dropdown. Supported fields are `id`, `name`, `gvk`, `gvkn`, `group`, `version`, `kind`, and `namespace`.
- **Enter** applies the current filter and closes the popup.
- **Escape** closes the popup without leaving the picker.

Outside the popup:

- **↑** / **↓** move the highlight in the list.
- **PgUp** / **PgDn** scroll the id list by one page.
- **s** toggles the sort direction and column. Each press switches between ascending (▲) and descending (▼) for the current column, then advances to the next column. **Shift+S** (capital **S**) runs the same cycle in reverse: descending → ascending on the same column, or previous column descending. The full cycle is: `id`↑ → `id`↓ → `kind`↑ → `kind`↓ → `namespace`↑ → `namespace`↓ → `name`↑ → `name`↓ → `id`↑ → … The active column header shows ▲ (ascending) or ▼ (descending).
- **Enter** opens the reference graph for the selected id.
- **Escape** or **q** exits without selecting.

The id list includes a **Status** column only for **`hydra gitops inspect`**: it reflects whether each id appears in rendered templates, in the live cluster inventory, or both. **`hydra local inspect`** does **not** show a status column in the picker.

## TUI Layout (reference graph)

The screen is split into zones: entity line, help/hotkey line (between the entity header and the list panel border), one full-width list (tree), and detail.

The **column header** row (**Dist**, **Ref id**, **Relation**, optional **Status**) stays **fixed** at the top of the list panel; only the lines below it scroll when the list is longer than the panel.

### Detail panel

The bordered **detail** area below the list shows the merged `Ref` for the **highlighted** row (from/to, labels, tags, description, attributes), word-wrapped to the panel width.

Hydra **sizes the detail panel so that the full ref text fits** whenever the terminal has enough vertical room. Long payloads are **not** cut off only because the detail area stayed unnecessarily small.

If the terminal is **too short** for both a usable reference list and the entire wrapped ref, Hydra **reduces the list height first** and gives the remaining space to the detail panel. If the ref still does not fit, the detail panel **scrolls internally** so you can read every line.

When you move the highlight to another row, the detail view **jumps back to the top** of that row’s ref.

**Incoming** and **outgoing** sections list **transitive** neighbors: Hydra walks the directed reference graph with **breadth-first search**, **up to 10 hop levels** per direction from the **current** entity—the same depth cap as the Hydra UI graph. The **current entity** appears **once** in the list at **Dist 0** so the anchor stays visible alongside related ids. Other rows show **hop distance** from the current id in the **`Dist`** column: **positive** values are **outgoing** hops along `From → To` edges; **negative** values are **incoming** hops (reverse traversal). This matches [`hydra local refs`](local/refs.md), [`hydra gitops refs`](cluster/refs.md), and the UI.

```text
Entity: apps/v1/Deployment/my-ns/my-app (ok)
  /: filter  s: sort  ↑/↓: navigate  PgUp/PgDn: page  Enter: follow  Esc: back  q: quit

┌──────────────────────────────────────────────────────────────────────────────────┐
│ Dist▲  Ref id                         Relation               Status                │
│ Incoming refs                                                                      │
│   ▸ -1   v1/ServiceAccount/my-ns/sa   sa                   ok                     │
│     -2   v1/Service/my-ns/svc         selector             template only          │
│                                                                                    │
│ Outgoing refs                                                                      │
│      0   apps/v1/Deployment/my-ns/my-app  (self)         —                       │
│      1   apps/v1/ReplicaSet/my-ns/rs      owner          cluster only            │
└──────────────────────────────────────────────────────────────────────────────────┘
```

The **Status** column is shown only in **`hydra gitops inspect`** (and in its id picker, see above). For each edge it summarizes how the merged ref was derived from **`origin:source`** attributes:

| Status | Meaning |
| ------ | ------- |
| `ok` | The edge appears in both the template render and the live cluster graph (`origin:source: template` and `origin:source: cluster`). |
| `template only` | The edge appears only from templates (`origin:source: template`); the same edge was not produced from live cluster entities. |
| `cluster only` | The edge appears only from the live cluster (`origin:source: cluster`), for example from runtime-only `metadata.ownerReferences` or other API-only refs. |
| `neither` | The merged ref has no `origin:source` from template or cluster (unusual; treated as neither side). |

- **Entity line** shows the canonical id of the entity currently being inspected. In **`hydra gitops inspect`**, it also shows **`(ok)`**, **`(template only)`**, **`(cluster only)`**, or **`(neither)`** in parentheses for the **highlighted** edge. **`hydra local inspect`** omits status there.

In the **id picker** for cluster-scoped graphs, each resource id is classified the same way against **template entity ids** vs **live cluster entity ids**: **`ok`**, **`template only`**, **`cluster only`**, or **`neither`** (for example an id that appears only via the ref graph endpoints, not as a standalone inventory document on either side).

- **List** uses the full terminal width. **Incoming refs** lists **transitively** reachable ids **toward** the current entity (negative **Dist**). **Outgoing refs** lists **transitively** reachable ids **away** from the current entity (positive **Dist**), **including** the current id at **Dist 0**. **`Dist`** is the **first** column in the list. Both sections appear in one scrollable block (incoming first, then outgoing) below the sticky column header. Use **PgUp** / **PgDn** to move the highlight by **one page** of rows, or `↑` / `↓` to move one row (the view scrolls to keep the highlighted row visible).
- **Dist** is the **BFS hop count** from the current entity in that direction, capped at **10** levels (peers beyond the cap do not appear).
- **Relation** shows the merged ref label (for example `namespace`, `serviceAccount`, or `owner`). If an edge has no label, Hydra falls back to the ref description.
- **Detail** (below the list) follows the rules in [Detail panel](#detail-panel) above (same for **`hydra local inspect`** and **`hydra gitops inspect`**).

## Filter popup (reference graph)

Press **`/`** while the reference graph is open to show a filter popup over the list/detail layout.

- The **search input** is focused by default.
- **Tab** moves focus to the **field dropdown**; **Shift+Tab** moves back.
- Supported fields are `id`, `name`, `gvk`, `gvkn`, `group`, `version`, `kind`, `namespace`, `relation`, and `status`.
- Filtering applies to the list rows. The detail panel continues to show the currently highlighted visible row.
- **Enter** applies the filter and closes the popup.
- **Escape** closes the popup without exiting the graph.
- Clearing the query shows all rows again.

## Sorting

Press **s** to toggle the sort direction and advance through columns. Each press switches between ascending (▲) and descending (▼) for the current column, then moves to the next column. Press **Shift+S** to run the reverse cycle (same as **s** but in the opposite direction).

- In the **id picker**, the full cycle is: `id`↑ → `id`↓ → `kind`↑ → `kind`↓ → `namespace`↑ → `namespace`↓ → `name`↑ → `name`↓ → `id`↑ → …
- In the **reference graph**, the default sort is **Dist** ascending. The sort cycle follows the **column order** left to right (**Dist**, **Ref id**, **Relation**, **Status**): `dist`↑ → `dist`↓ → `id`↑ → `id`↓ → `relation`↑ → `relation`↓ → `status`↑ → `status`↓ → `dist`↑ → …
- The active column header displays **▲** (ascending) or **▼** (descending); other columns show a **space** in that slot so headers stay aligned when you change the sort column.
- Sorting only changes the visible order. It does not change the underlying graph or navigation history.

## Keyboard Controls (reference graph)

**List vs detail:** **PgUp** and **PgDn** always page the **reference list**, not the detail text. When the detail panel holds more lines than fit on screen, Hydra shows **additional** key bindings in the **help line** at the top of the screen; those bindings scroll **only** the detail text. The help line is authoritative for the exact keys in your build.

| Key | Action |
| --- | ------ |
| `↑` / `↓` | Move cursor through the currently visible rows (list scrolls to keep the selection visible) |
| `PgUp` / `PgDn` | Scroll the reference list by one page when it is taller than the list panel |
| `/` | Open the filter popup |
| `s` | Toggle sort direction (ascending ▲ / descending ▼) for the current column, then advance to the next column |
| `Shift+S` | Same as **s**, but reverse direction (descending → ascending on the same column, or previous column descending) |
| **Detail text scroll** | When wrapped ref text is taller than the detail panel, use the keys named in the **help line** at the top (they do not replace list paging; **PgUp**/**PgDn** still affect the list only) |
| `Enter` | Navigate into the selected entity (push onto history stack) |
| `Escape` | Close the popup, or go back to the previous entity when no popup is open |
| `q` | Quit the TUI |

When you start **`hydra local inspect`** or **`hydra gitops inspect`** with only `<cluster>` (no `<id>`), the id picker runs first. If you then press **Escape** on the **root** entity (no navigation history yet), Hydra returns to the **id picker** instead of exiting. When you start the command **with** an `<id>`, **Escape** at the root entity exits the TUI (same as **q**).

When you press **Enter**, the selected entity becomes the new current
entity. Its incoming and outgoing refs are loaded and displayed. The
previous entity is pushed onto an internal history stack so **Escape**
returns to it. The stack is unlimited; pressing **Escape** at the root entity
either returns to the id picker (when you started without `<id>`) or exits the TUI
(when you started with an explicit `<id>`), as described above.

Picker and graph use **Tab** only while the filter popup is open. Outside the popup, the graph keeps its single combined list.

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## How Refs Are Resolved

The inspect command computes the full reference graph for all effectively
enabled applications on the cluster using the same ref-parser pipeline as
[`hydra local refs`](local/refs.md): CEL-based ref-parsers
extract `RefDefinition` entries from entities, which are connected into
`Ref` edges. The TUI keeps that graph in memory and, for each **current**
entity, derives the **transitive** incoming and outgoing frontiers (**BFS**, **max
10** levels) to populate the list and **Dist** column—analogous to the Hydra UI
graph.

- **`hydra local inspect`** — Refs come **only** from Helm-rendered template entities. Each emitted `Ref` includes attribute **`origin:source: template`**.
- **`hydra gitops inspect`** — Refs are computed **twice** (templates and live cluster inventory), merged, and deduplicated by edge identity. Each edge is tagged with **`origin:source: template`** and/or **`origin:source: cluster`** so you can see whether an edge was derived from manifests, from API objects (for example `metadata.ownerReferences` on runtime Pods), or both.

When you navigate to a new entity, the TUI recomputes the BFS frontiers from the cached graph for the new id without re-rendering Helm templates or re-listing the cluster.

## Examples

```bash
# Open the id picker, then pick a resource (local templates)
hydra local inspect prod

# Browse refs for a Deployment on cluster prod (offline, local templates)
hydra local inspect prod apps/v1/Deployment/my-ns/my-app

# Picker including live cluster objects, then graph
hydra gitops inspect prod

# Browse refs on a live cluster
hydra gitops inspect prod apps/v1/Deployment/my-ns/my-app

# Offline chart resolution
hydra local inspect prod v1/ConfigMap/my-ns/my-cm --helm-network-mode offline
```

## See Also

- [`hydra local refs`](local/refs.md) — transitive reference listing (templates only) as YAML
- [`hydra gitops refs`](cluster/refs.md) — transitive listing on the merged template+cluster graph
- [`hydra local review`](local/review.md) - validate reference integrity offline
- [`hydra gitops review`](cluster/review.md) - validate reference integrity against live cluster
