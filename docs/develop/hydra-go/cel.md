# CEL Expression Architecture

Hydra uses the Common Expression Language (CEL) for discovering references between entities (ref-parsers), filtering/selecting entities (predicates), and projecting rendered entities to YAML-serializable values for commands such as `hydra local find`. The CEL package provides a configured environment with custom functions, entity support, and program compilation.

## Key Concepts

- **CEL Environment** — Configured with standard extensions (encoders, strings, lists, regex, math, sets), entity support, utility functions, and ref-builder support
- **Expression** — Evaluates a CEL expression against an entity, returns any value; used for ref-parser `pick` expressions
- **Predicate** — Evaluates a CEL expression returning a boolean; used for entity filtering (`--include`, `--exclude`)
- **Projection expression** — Evaluates a CEL expression returning a YAML-serializable value (string, bool, number, list, or map); used by `hydra local find --pick`
- **Programs** — Combines multiple predicates with AND logic
- **RefBuilder** — Custom CEL type that accumulates reference definitions via a fluent builder API
- **Entity-to-CEL conversion** — Entities are flattened to maps with all EntityKey values plus the full unstructured resource
- **Entity inventory CEL (`managedNamespaces`, `templateEntities`, `clusterEntities`, `entities`)** — Functions registered with **`ClusterInventorySupport`** / **`NewEnvWithEntityInventory`** expose template and cluster entity snapshots and the sorted deduplicated managed-namespace name list (replacement for the removed **`HydraManagedNamespaces`** variable). **Uninstall predicate compilation paths** (`uninstall`/`backup`, `uninstall-safe`, `uninstall-force`) must build the CEL environment with **`cel.NewEnvWithEntityInventory(rendered)`** using the **same** render as predicate collection when app-defined predicates use these helpers. See [details/cel.md — Entity inventory and managed namespaces](details/cel.md#entity-inventory-and-managed-namespaces-uninstall--backup--refs--clones).
- **global.hydra.ready** — Readiness rules for scale status and scale-up gating: per named entry, a CEL **`predicate`** selects entities; a **`cel`** YAML list of expressions must **all** pass for **ready**. Each expression returns **`null`** (omit check), **`""`** (pass), a **non-empty string** (one failure reason), or a **list of strings** (multiple failure reasons, flattened into `readyMessages`). **`templateEntities()`**, **`clusterEntities()`**, **`entities()`** and their selector-object overloads, plus **`involvedObjectEvents(...)`**, expose merged render+live inventory where applicable for correlating workloads with cluster state and `Event` objects. Same top-level entity variables as ref-parser predicates. See [details/cel.md — global.hydra.ready rules](details/cel.md#global-hydra-ready-rules-predicate-and-cel-list) and [values.md](details/values.md).

For CLI usage, note that `hydra local find --pick` is separate from the `pick:` field used in ref-parser YAML. They reuse the same expression engine but serve different workflows.

## Source Files

`core/cel/env.go`, `core/cel/expression.go`, `core/cel/predicate.go`, `core/cel/program.go`, `core/cel/programs.go`, `core/cel/entity_support.go`, `core/cel/service_support.go`, `core/cel/cluster_inventory_support.go`, `core/cel/util_support.go`, `core/cel/list_support.go`, `core/cel/ref_type.go`, `core/cel/value_type.go`

→ **Full details:** [details/cel.md](details/cel.md)
